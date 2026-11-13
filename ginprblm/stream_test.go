package ginprblm_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/ginprblm"
)

// gin-adapter / Scenario: Streaming and hijacking, SSE row: a flushed event
// reaches the client while the handler is still running.
func TestDeferredWriter_Commit_SSE(t *testing.T) {
	t.Parallel()

	engine := newEngine(func(c *gin.Context) {
		c.SSEvent("message", "1")
		c.Writer.Flush()

		// The handler stays alive until the client, having read the event
		// (or timed out), goes away.
		<-c.Request.Context().Done()
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/x", http.NoBody)
	req.RequestURI = ""

	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}

	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "data:1" {
			return // received before the handler returned
		}
	}

	t.Fatalf("event not received while the handler ran: %v", scanner.Err())
}

// gin-adapter / Scenario: Streaming and hijacking, flush and WriteHeaderNow
// rows.
func TestDeferredWriter_Commit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler gin.HandlerFunc
		hookErr error
	}{
		{
			name: "flush then error",
			handler: func(c *gin.Context) {
				c.Status(http.StatusOK)
				c.Writer.Flush()
				_ = c.Error(ErrLocked)
			},
			hookErr: ErrLocked,
		},
		{
			name: "write header now without body",
			handler: func(c *gin.Context) {
				c.Status(http.StatusOK)
				c.Writer.WriteHeaderNow()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hook hookRecorder

			rec := fetch(t, newEngine(tt.handler, hook.option()), "/x")

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}

			if body := rec.Body.String(); body != "" {
				t.Errorf("body = %q, want empty", body)
			}

			assertHook(t, &hook, tt.hookErr)
		})
	}
}

// gin-adapter / Scenario: Streaming and hijacking, hijack row: after a
// websocket-style upgrade the library writes nothing to the connection and
// the recorded error only reaches the hook.
func TestDeferredWriter_Hijack(t *testing.T) {
	t.Parallel()

	const upgraded = "HTTP/1.1 101 Switching Protocols\r\nUpgrade: test\r\nConnection: Upgrade\r\n\r\nhello"

	var hook hookRecorder

	// The server does not track hijacked connections, so done tells when
	// the whole chain, Middleware included, has returned.
	done := make(chan struct{})

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		defer close(done)

		c.Next()
	})
	engine.Use(ginprblm.Middleware(prblm.NewHandler(hook.option())))
	engine.GET("/x", func(c *gin.Context) {
		conn, rw, err := c.Writer.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)

			return
		}

		_, _ = rw.WriteString(upgraded)
		_ = rw.Flush()
		_ = conn.Close()

		_ = c.Error(ErrLocked)
	})

	// Read only after done: the log is written before the chain returns.
	var serverLog bytes.Buffer

	srv := httptest.NewUnstartedServer(engine)
	srv.Config.ErrorLog = log.New(&serverLog, "", 0)
	srv.Start()
	t.Cleanup(srv.Close)

	var dialer net.Dialer

	conn, err := dialer.DialContext(t.Context(), "tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer func() { _ = conn.Close() }()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	_, err = io.WriteString(conn, "GET /x HTTP/1.1\r\nHost: test\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
	if err != nil {
		t.Fatalf("write request: %v", err)
	}

	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if string(got) != upgraded {
		t.Errorf("client received %q, want only the handler's %q", got, upgraded)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler chain did not return")
	}

	if msg := serverLog.String(); strings.Contains(msg, "hijacked") {
		t.Errorf("library wrote to the hijacked connection: %s", msg)
	}

	assertHook(t, &hook, ErrLocked)
}

// Murphy row: handlers keep reaching the connection's capabilities through
// http.ResponseController with the middleware installed, as with gin's own
// writer.
func TestDeferredWriter_ResponseController(t *testing.T) {
	t.Parallel()

	engine := newEngine(func(c *gin.Context) {
		rc := http.NewResponseController(c.Writer)

		if err := rc.SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
			t.Errorf("SetWriteDeadline: %v", err)
		}

		if err := rc.EnableFullDuplex(); err != nil {
			t.Errorf("EnableFullDuplex: %v", err)
		}

		c.String(http.StatusOK, "ok")
	})

	if rec := fetch(t, engine, "/x"); rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("response = %d %q, want 200 %q", rec.Code, rec.Body.String(), "ok")
	}
}
