package prblm_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/kartaladev/prblm"
)

func ExampleHandler_Wrap() {
	errNotFound := errors.New("vessel not found")

	h := prblm.NewHandler(prblm.WithMappers(
		prblm.Status(errNotFound, http.StatusNotFound),
	))

	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
		return fmt.Errorf("load vessel 7: %w", errNotFound)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vessels/7?token=secret", http.NoBody)
	handler.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Content-Type"))
	fmt.Println(rec.Body.String())
	// Output:
	// 404
	// application/problem+json
	// {"title":"Not Found","status":404,"instance":"/v1/vessels/7"}
}

func ExampleHandler_Write() {
	errNotFound := errors.New("vessel not found")

	h := prblm.NewHandler(prblm.WithMappers(
		prblm.Status(errNotFound, http.StatusNotFound),
	))

	// A standard handler, for example one that is already registered with a
	// router and cannot change its signature.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "1" {
			h.Write(w, r, errNotFound)

			return
		}

		_, _ = w.Write([]byte(`{"id":1}`))
	})

	mux := http.NewServeMux()
	mux.Handle("GET /v1/vessels/{id}", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vessels/7", http.NoBody)
	mux.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Content-Type"))
	fmt.Println(rec.Header().Get("Cache-Control"))
	fmt.Println(rec.Body.String())
	// Output:
	// 404
	// application/problem+json
	// no-store
	// {"title":"Not Found","status":404,"instance":"/v1/vessels/7"}
}

func ExampleHandler_Recover() {
	// Log without timestamps, so the output is stable.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}

			return a
		},
	}))

	h := prblm.NewHandler(prblm.OnError(func(ctx context.Context, err error, _ *prblm.Problem) {
		if pe, ok := errors.AsType[*prblm.PanicError](err); ok {
			// pe.Stack holds the stack at the panic; log it in production.
			logger.ErrorContext(ctx, "handler panicked", "value", pe.Value)

			return
		}

		logger.ErrorContext(ctx, "request failed", "err", err)
	}))

	handler := h.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("vessel cache not initialized")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vessels/7", http.NoBody)
	handler.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Body.String())
	// Output:
	// level=ERROR msg="handler panicked" value="vessel cache not initialized"
	// 500
	// {"title":"Internal Server Error","status":500,"instance":"/v1/vessels/7"}
}

func ExampleParse() {
	errNotFound := errors.New("vessel not found")

	h := prblm.NewHandler(prblm.WithMappers(prblm.Status(errNotFound, http.StatusNotFound)))

	srv := httptest.NewServer(h.Wrap(func(http.ResponseWriter, *http.Request) error {
		return errNotFound
	}))
	defer srv.Close()

	// A client of the service, or a test of it.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/v1/vessels/7", http.NoBody)
	if err != nil {
		fmt.Println(err)

		return
	}

	res, err := srv.Client().Do(req) //nolint:bodyclose // Parse closes the body.
	if err != nil {
		fmt.Println(err)

		return
	}

	p, err := prblm.Parse(res) // closes the body
	if errors.Is(err, prblm.ErrNotProblem) {
		fmt.Println("not a problem response")

		return
	}

	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(p.Status, p.Title, p.Instance)
	// Output:
	// 404 Not Found /v1/vessels/7
}

func ExampleNew() {
	p := prblm.New(http.StatusForbidden,
		prblm.WithType("https://example.com/probs/out-of-credit"),
		prblm.WithTitle("You do not have enough credit."),
		prblm.WithDetail("Your current balance is 30, but that costs 50."),
		prblm.WithExt("balance", 30),
		prblm.WithExt("accounts", []string{"/account/12345", "/account/67890"}),
		prblm.WithHeader("Retry-After", "3600"),
		prblm.WithCause(errors.New("ledger: insufficient funds")),
	)

	body, err := json.Marshal(p)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(string(body))
	fmt.Println(p.Header.Get("Retry-After"))
	fmt.Println(p)
	// Output:
	// {"type":"https://example.com/probs/out-of-credit","title":"You do not have enough credit.","status":403,"detail":"Your current balance is 30, but that costs 50.","accounts":["/account/12345","/account/67890"],"balance":30}
	// 3600
	// You do not have enough credit.: Your current balance is 30, but that costs 50.
}

func ExampleProblem_MarshalXML() {
	p := prblm.New(http.StatusForbidden,
		prblm.WithType("https://example.com/probs/out-of-credit"),
		prblm.WithTitle("You do not have enough credit."),
		prblm.WithExt("balance", 30),
		prblm.WithExt("accounts", []string{"/account/12345", "/account/67890"}),
	)

	body, err := xml.Marshal(p)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(string(body))
	// Output:
	// <problem xmlns="urn:ietf:rfc:7807"><type>https://example.com/probs/out-of-credit</type><title>You do not have enough credit.</title><status>403</status><accounts><i>/account/12345</i><i>/account/67890</i></accounts><balance>30</balance></problem>
}

func ExampleProblem_Set() {
	p := prblm.New(http.StatusUnprocessableEntity)

	fmt.Println(p.Set("trace_id", "abc"))
	fmt.Println(p.Set("trace-id", "abc"))
	fmt.Println(p.Set("status", 500))
	// Output:
	// <nil>
	// prblm: invalid extension name: "trace-id"
	// prblm: reserved extension name: "status"
}

func ExampleIs() {
	errNotFound := errors.New("vessel not found")

	h := prblm.NewHandler(prblm.WithMappers(
		prblm.Is(errNotFound, func(error) *prblm.Problem {
			return prblm.New(http.StatusNotFound, prblm.WithDetail("No vessel has that ID."))
		}),
	))

	p := h.Resolve(context.Background(), fmt.Errorf("load vessel 7: %w", errNotFound))
	fmt.Println(p.Status, p.Title, p.Detail)
	// Output:
	// 404 Not Found No vessel has that ID.
}

// RefusedError is a domain error that carries data for the client.
type RefusedError struct{ Reasons []string }

func (e *RefusedError) Error() string { return "refused" }

func ExampleAs() {
	h := prblm.NewHandler(prblm.WithMappers(
		prblm.As(func(e *RefusedError) *prblm.Problem {
			return prblm.New(http.StatusConflict, prblm.WithExt("reasons", e.Reasons))
		}),
	))

	p := h.Resolve(context.Background(), fmt.Errorf("book berth: %w", &RefusedError{Reasons: []string{"closed"}}))

	body, _ := json.Marshal(p)
	fmt.Println(string(body))
	// Output:
	// {"title":"Conflict","status":409,"reasons":["closed"]}
}

func ExampleWithStatus() {
	h := prblm.NewHandler()

	// No mapper knows this error, but the caller knows it means "forbidden".
	err := prblm.WithStatus(errors.New("tenant 42 may not read vessel 7"), http.StatusForbidden)

	body, _ := json.Marshal(h.Resolve(context.Background(), err))
	fmt.Println(string(body))
	// Output:
	// {"title":"Forbidden","status":403}
}

func ExampleScopeMappers() {
	errLocked := errors.New("locked")
	h := prblm.NewHandler()

	// A middleware on the contract routes maps errLocked for them only.
	contracts := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := prblm.ScopeMappers(r.Context(), prblm.Status(errLocked, http.StatusLocked))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

	fail := h.Wrap(func(http.ResponseWriter, *http.Request) error { return errLocked })

	mux := http.NewServeMux()
	mux.Handle("/v1/contracts/", contracts(fail))
	mux.Handle("/v1/vessels/", fail)

	for _, path := range []string{"/v1/contracts/1", "/v1/vessels/1"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, http.NoBody))
		fmt.Println(path, rec.Code)
	}
	// Output:
	// /v1/contracts/1 423
	// /v1/vessels/1 500
}

func ExampleOnError() {
	h := prblm.NewHandler(prblm.OnError(func(_ context.Context, err error, p *prblm.Problem) {
		// Log here; the client never sees err.
		fmt.Printf("answered %d for: %v\n", p.Status, err)
	}))

	res, _ := h.Respond(context.Background(), prblm.Request{Path: "/v1/vessels/7"}, errors.New("pq: connection refused"))
	fmt.Println(string(res.Body))
	// Output:
	// answered 500 for: pq: connection refused
	// {"title":"Internal Server Error","status":500,"instance":"/v1/vessels/7"}
}

func ExampleWithNamespace() {
	errBlacklisted := errors.New("supplier is blacklisted")

	// Business code names only the sub-type; the namespace makes it a URL.
	blacklisted := prblm.WithMappers(prblm.Is(errBlacklisted, func(error) *prblm.Problem {
		return prblm.New(http.StatusConflict, prblm.WithType("contracts/blacklisted-supplier"))
	}))

	for _, ns := range []string{"/v1/api/probs", "https://kartala.id/probs"} {
		h := prblm.NewHandler(prblm.WithNamespace(ns), blacklisted)

		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/contracts", http.NoBody)
		h.Write(rec, req, errBlacklisted)

		fmt.Println(rec.Body.String())
	}
	// Output:
	// {"type":"/v1/api/probs/contracts/blacklisted-supplier","status":409,"instance":"/v1/contracts"}
	// {"type":"https://kartala.id/probs/contracts/blacklisted-supplier","status":409,"instance":"/v1/contracts"}
}

// BlacklistedSupplier is defined once, at package level, and shared by every
// mapper that answers with it.
var BlacklistedSupplier = prblm.DefineType(
	"contracts/blacklisted-supplier",
	http.StatusConflict,
	"Supplier is blacklisted",
	"The supplier is on the procurement blacklist and cannot be added to a contract.",
)

func ExampleDefineType() {
	errBlacklisted := errors.New("supplier is blacklisted")

	h := prblm.NewHandler(
		prblm.WithNamespace("/v1/api/probs"),
		prblm.WithTypes(BlacklistedSupplier),
		prblm.WithMappers(prblm.Is(errBlacklisted, func(error) *prblm.Problem {
			return BlacklistedSupplier.New(prblm.WithExt("supplier_id", 42))
		})),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/contracts", http.NoBody)
	h.Write(rec, req, fmt.Errorf("add supplier 42: %w", errBlacklisted))

	fmt.Println(rec.Code)
	fmt.Println(rec.Body.String())
	// Output:
	// 409
	// {"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"instance":"/v1/contracts","supplier_id":42}
}

func ExampleHandler_Discovery() {
	outOfCredit := prblm.DefineType("out-of-credit", http.StatusForbidden, "Out of credit",
		"The account balance is below the price of the request.")

	h := prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), prblm.WithTypes(outOfCredit))

	mux := http.NewServeMux()
	// Serve the documentation of every registered type at its type URL.
	if path := h.DiscoveryPath(); path != "" {
		mux.Handle("GET "+path+"/", h.Discovery())
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/api/probs/out-of-credit", http.NoBody)
	req.Header.Set("Accept", "application/json")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	fmt.Println(rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	fmt.Println(rec.Body.String())
	// Output:
	// 200 application/json public, max-age=3600
	// {"type":"/v1/api/probs/out-of-credit","subtype":"out-of-credit","status":403,"title":"Out of credit","description":"The account balance is below the price of the request."}
}
