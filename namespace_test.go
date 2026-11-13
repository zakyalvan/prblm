package prblm_test

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kartaladev/prblm"
)

// errContract is the error the type resolution tests answer.
var errContract = errors.New("contract rejected")

// lockedDetail is the detail of the problems lockedOfType returns.
const lockedDetail = "Contract 7 is locked."

// lockedOfType returns a 409 problem of type typ, titled Locked, with the
// detail lockedDetail.
func lockedOfType(typ string) *prblm.Problem {
	return prblm.New(http.StatusConflict, prblm.WithType(typ), prblm.WithTitle("Locked"), prblm.WithDetail(lockedDetail))
}

// conflictOfType returns a mapper that answers errContract with
// lockedOfType(typ).
func conflictOfType(typ string) prblm.Option {
	return prblm.WithMappers(prblm.Is(errContract, func(error) *prblm.Problem { return lockedOfType(typ) }))
}

// answer sends err through h.Wrap for GET /v1/contracts/7 and returns the
// response.
func answer(t *testing.T, h *prblm.Handler, err error) *httptest.ResponseRecorder {
	t.Helper()

	return answerRequest(h, err, contractRequest(t.Context()))
}

// contractRequest returns GET /v1/contracts/7 with ctx.
func contractRequest(ctx context.Context) *http.Request {
	return httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/contracts/7", http.NoBody)
}

// answerRequest sends err through h.Wrap for req and returns the response.
func answerRequest(h *prblm.Handler, err error, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.Wrap(func(http.ResponseWriter, *http.Request) error { return err }).ServeHTTP(rec, req)

	return rec
}

// typeMember returns the type member of the JSON problem in rec, or "" when
// it has none.
func typeMember(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	typ, _ := decodeMembers(t, rec.Body.Bytes())["type"].(string)

	return typ
}

// problem-types / Scenario: Namespace validated.
func TestWithNamespace_Validation(t *testing.T) {
	t.Parallel()

	accepted := []string{
		"/v1/api/probs",
		"/v1/api/probs/",
		"https://kartala.id/probs",
		"http://localhost:8080/probs",
	}

	for _, ns := range accepted {
		t.Run("accepted "+ns, func(t *testing.T) {
			t.Parallel()

			prblm.NewHandler(prblm.WithNamespace(ns))
		})
	}

	rejected := []string{
		"",
		"v1/api/probs",
		"/",
		"//evil.example/probs",
		"https://kartala.id",
		"https://kartala.id/",
		"https:///probs",
		"ftp://kartala.id/probs",
		"https://user@kartala.id/probs",
		"https://kartala.id/probs?v=1",
		"/v1/api/probs#x",
		"/v1/../probs",
		"/v1/api probs",
	}

	for _, ns := range rejected {
		t.Run("rejected "+ns, func(t *testing.T) {
			t.Parallel()

			msg := mustPanic(t, func() { prblm.NewHandler(prblm.WithNamespace(ns)) })

			if !strings.Contains(msg, "WithNamespace") || !strings.Contains(msg, `"`+ns+`"`) {
				t.Errorf("panic = %q, want a message naming WithNamespace and %q", msg, ns)
			}
		})
	}
}

// problem-types / Scenario: Namespace option order.
func TestWithNamespace_Order(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		first     string
		last      string
		namespace string
	}{
		{name: "invalid replaced by valid", first: "bad", last: "/v1/api/probs", namespace: "/v1/api/probs"},
		{name: "valid replaced by valid", first: "/a/probs", last: "/v1/api/probs", namespace: "/v1/api/probs"},
		{name: "valid replaced by invalid", first: "/v1/api/probs", last: "bad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := []prblm.Option{prblm.WithNamespace(tt.first), prblm.WithNamespace(tt.last), conflictOfType("x")}

			if tt.namespace == "" {
				msg := mustPanic(t, func() { prblm.NewHandler(opts...) })
				if !strings.Contains(msg, "WithNamespace") || !strings.Contains(msg, `"`+tt.last+`"`) {
					t.Errorf("panic = %q, want a message naming WithNamespace and %q", msg, tt.last)
				}

				return
			}

			rec := answer(t, prblm.NewHandler(opts...), errContract)
			if got, want := typeMember(t, rec), tt.namespace+"/x"; got != want {
				t.Errorf("type = %q, want %q", got, want)
			}
		})
	}
}

// problem-types / Scenario: Type resolved.
func TestResolve_Namespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		given     string
		rendered  string
	}{
		{name: "resolved path", namespace: "/v1/api/probs", given: "contracts/blacklisted-supplier", rendered: "/v1/api/probs/contracts/blacklisted-supplier"},
		{name: "resolved uri", namespace: "https://kartala.id/probs", given: "contracts/blacklisted-supplier", rendered: "https://kartala.id/probs/contracts/blacklisted-supplier"},
		{name: "resolved single segment", namespace: "/v1/api/probs", given: "out-of-credit", rendered: "/v1/api/probs/out-of-credit"},
		{name: "resolved trailing slash", namespace: "/v1/api/probs/", given: "out-of-credit", rendered: "/v1/api/probs/out-of-credit"},
		{name: "resolved digits", namespace: "https://kartala.id/probs", given: "a/b/c-2", rendered: "https://kartala.id/probs/a/b/c-2"},
		{name: "kept absolute uri", namespace: "/v1/api/probs", given: "https://example.com/probs/x", rendered: "https://example.com/probs/x"},
		{name: "kept absolute path", namespace: "/v1/api/probs", given: "/other/probs/x", rendered: "/other/probs/x"},
		{name: "kept about:blank", namespace: "/v1/api/probs", given: "about:blank"},
		{name: "kept empty", namespace: "/v1/api/probs"},
		{name: "kept urn", namespace: "/v1/api/probs", given: "urn:kartala:probs:x", rendered: "urn:kartala:probs:x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithNamespace(tt.namespace), conflictOfType(tt.given))
			rec := answer(t, h, errContract)

			if rec.Code != http.StatusConflict {
				t.Errorf("status = %d, want 409", rec.Code)
			}

			if got := typeMember(t, rec); got != tt.rendered {
				t.Errorf("type = %q, want %q", got, tt.rendered)
			}
		})
	}
}

// problem-types / Scenario: No namespace.
func TestResolve_NoNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		given    string
		rendered string
		isNoted  bool
	}{
		{name: "sub-type", given: "contracts/blacklisted-supplier", rendered: "contracts/blacklisted-supplier", isNoted: true},
		{name: "invalid sub-type", given: "Contracts/X", rendered: "Contracts/X", isNoted: true},
		{name: "absolute uri", given: "https://example.com/probs/x", rendered: "https://example.com/probs/x"},
		{name: "absolute path", given: "/other/probs/x", rendered: "/other/probs/x"},
		{name: "about:blank", given: "about:blank"},
		{name: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			h := prblm.NewHandler(conflictOfType(tt.given), prblm.OnError(r.hook))
			rec := answer(t, h, errContract)

			if got := typeMember(t, rec); got != tt.rendered {
				t.Errorf("type = %q, want %q", got, tt.rendered)
			}

			assertTypeNote(t, r.recorded(), tt.isNoted, errContract)
		})
	}
}

// assertTypeNote checks that the hook calls are the answered error alone,
// or, when isNoted, a note wrapping ErrInvalidType followed by it.
func assertTypeNote(t *testing.T, calls []hookCall, isNoted bool, answered error) {
	t.Helper()

	//nolint:errorlint // the last call must be the answered error itself.
	switch {
	case isNoted && (len(calls) != 2 || !errors.Is(calls[0].err, prblm.ErrInvalidType) || calls[1].err != answered):
		t.Errorf("hook calls = %+v, want an ErrInvalidType note, then the answered error", calls)
	case !isNoted && (len(calls) != 1 || calls[0].err != answered):
		t.Errorf("hook calls = %+v, want only the call with the answered error", calls)
	}
}

// problem-types / Scenario: Problem from a definition without a namespace.
func TestType_New_NoNamespace(t *testing.T) {
	t.Parallel()

	var r recorder

	h := prblm.NewHandler(prblm.OnError(r.hook), prblm.WithMappers(
		prblm.Is(errContract, func(error) *prblm.Problem { return outOfCredit.New() }),
	))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/orders/42", http.NoBody)
	rec := answerRequest(h, errContract, req)

	if got := typeMember(t, rec); got != "out-of-credit" {
		t.Errorf("type = %q, want %q", got, "out-of-credit")
	}

	assertTypeNote(t, r.recorded(), true, errContract)
}

// problem-types / Scenario: Hostile host headers.
func TestResolve_HostHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		header    http.Header
		rendered  string
	}{
		{
			name:      "host on a path namespace",
			namespace: "/v1/api/probs",
			header:    http.Header{"Host": {"evil.example"}},
			rendered:  "/v1/api/probs/contracts/blacklisted-supplier",
		},
		{
			name:      "forwarded headers on a path namespace",
			namespace: "/v1/api/probs",
			header:    http.Header{"X-Forwarded-Host": {"evil.example"}, "X-Forwarded-Proto": {"http"}},
			rendered:  "/v1/api/probs/contracts/blacklisted-supplier",
		},
		{
			name:      "host on a uri namespace",
			namespace: "https://kartala.id/probs",
			header:    http.Header{"Host": {"evil.example"}},
			rendered:  "https://kartala.id/probs/contracts/blacklisted-supplier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithNamespace(tt.namespace), conflictOfType("contracts/blacklisted-supplier"))
			req := contractRequest(t.Context())
			maps.Copy(req.Header, tt.header)

			if host := tt.header.Get("Host"); host != "" {
				req.Host = host
			}

			rec := answerRequest(h, errContract, req)

			if got := typeMember(t, rec); got != tt.rendered {
				t.Errorf("type = %q, want %q", got, tt.rendered)
			}
		})
	}
}

// typeSource is a way a problem with a given type reaches the handler: the
// options and context it needs, and the error that is answered.
type typeSource struct {
	opts  []prblm.Option
	ctx   func(ctx context.Context) context.Context
	err   error
	label string
}

// selfDescribing is the source of a 409 problem of type typ returned as the
// error itself.
func selfDescribing(typ string) typeSource {
	return typeSource{label: "a self-describing error", err: lockedOfType(typ)}
}

// scopedMapper is the source of a 409 problem of type typ from a mapper
// scoped to the request.
func scopedMapper(typ string) typeSource {
	mapper := prblm.Is(errContract, func(error) *prblm.Problem { return lockedOfType(typ) })

	return typeSource{
		label: "a scoped mapper",
		err:   errContract,
		ctx:   func(ctx context.Context) context.Context { return prblm.ScopeMappers(ctx, mapper) },
	}
}

// handlerMapper is the source of a 409 problem of type typ from a mapper of
// the handler.
func handlerMapper(typ string) typeSource {
	return typeSource{label: "a handler mapper", err: errContract, opts: []prblm.Option{conflictOfType(typ)}}
}

// enricher is the source of a mapped 409 problem whose type an enricher
// sets to typ.
func enricher(typ string) typeSource {
	return typeSource{
		label: "an enricher",
		err:   errContract,
		opts: []prblm.Option{
			conflictOfType(""),
			prblm.WithEnrichers(func(_ context.Context, p *prblm.Problem) { p.Type = typ }),
		},
	}
}

// answerFrom answers the error of src through a handler with namespace
// /v1/api/probs, src's options and the extra options.
func answerFrom(t *testing.T, src typeSource, extra ...prblm.Option) *httptest.ResponseRecorder {
	t.Helper()

	opts := append([]prblm.Option{prblm.WithNamespace("/v1/api/probs")}, src.opts...)
	h := prblm.NewHandler(append(opts, extra...)...)

	ctx := t.Context()
	if src.ctx != nil {
		ctx = src.ctx(ctx)
	}

	return answerRequest(h, src.err, contractRequest(ctx))
}

// assertConflictWithoutType checks that rec is the 409 problem of
// lockedOfType after its type became about:blank: no type member, the
// status phrase Conflict as title instead of Locked, which was written for
// the lost type, and the detail kept.
func assertConflictWithoutType(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	m := decodeMembers(t, rec.Body.Bytes())

	_, hasType := m["type"]
	if hasType || rec.Code != http.StatusConflict || m["title"] != "Conflict" || m["detail"] != lockedDetail {
		t.Errorf("response %d %v, want 409 titled Conflict with detail %q and without a type", rec.Code, m, lockedDetail)
	}
}

// problem-types / Scenario: Invalid relative type.
func TestResolve_InvalidType(t *testing.T) {
	t.Parallel()

	var sources []typeSource

	for _, given := range []string{
		"Contracts/X", "contracts//x", "contracts/", "../admin", "./x",
		"contracts/x?y=1", "contracts/x#y", "x y", "x--y", "-x",
	} {
		src := handlerMapper(given)
		src.label += " " + given
		sources = append(sources, src)
	}

	sources = append(sources, selfDescribing("Bad Type"), scopedMapper("Bad Type"), enricher("Bad Type"))

	for _, src := range sources {
		t.Run(src.label, func(t *testing.T) {
			t.Parallel()

			var r recorder

			rec := answerFrom(t, src, prblm.OnError(r.hook))
			assertConflictWithoutType(t, rec)
			assertTypeNote(t, r.recorded(), true, src.err)
		})
	}
}

// Murphy row for problem-types / Scenario: Invalid relative type: a problem
// with an invalid type and no title becomes about:blank, so it gets the
// reason phrase of its status as title, like any about:blank problem.
func TestResolve_InvalidType_NoTitle(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), prblm.WithMappers(
		prblm.Is(errContract, func(error) *prblm.Problem {
			return prblm.New(http.StatusConflict, prblm.WithType("Bad Type"))
		}),
	))

	m := decodeMembers(t, answer(t, h, errContract).Body.Bytes())

	if _, ok := m["type"]; ok || m["title"] != "Conflict" {
		t.Errorf("members = %v, want no type and the title Conflict", m)
	}
}

// problem-types / Scenario: Faulty hook on a type note.
func TestResolve_InvalidType_FaultyHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		hook func(ctx context.Context, err error, p *prblm.Problem)
		name string
	}{
		{
			name: "panics on every call",
			hook: func(context.Context, error, *prblm.Problem) { panic("hook") },
		},
		{
			name: "panics only on the note",
			hook: func(_ context.Context, err error, _ *prblm.Problem) {
				if errors.Is(err, prblm.ErrInvalidType) {
					panic("hook")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertConflictWithoutType(t, answerFrom(t, handlerMapper("Bad Type"), prblm.OnError(tt.hook)))
		})
	}
}

// problem-types / Scenario: Problem sources.
func TestResolve_Sources(t *testing.T) {
	t.Parallel()

	sources := []typeSource{
		selfDescribing("contracts/locked"),
		scopedMapper("contracts/locked"),
		handlerMapper("contracts/locked"),
		enricher("contracts/locked"),
	}

	for _, src := range sources {
		t.Run(src.label, func(t *testing.T) {
			t.Parallel()

			rec := answerFrom(t, src)

			if got := typeMember(t, rec); rec.Code != http.StatusConflict || got != "/v1/api/probs/contracts/locked" {
				t.Errorf("response %d with type %q, want 409 with /v1/api/probs/contracts/locked", rec.Code, got)
			}
		})
	}
}

// sharedLocked is a package-level problem, shared by every request that
// returns it.
//
//nolint:errname // a problem value, not a sentinel compared with errors.Is.
var sharedLocked = lockedOfType("contracts/locked")

// problem-types / Scenario: Shared problem stays relative.
func TestResolve_SharedProblem(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(
		prblm.WithNamespace("/v1/api/probs"),
		prblm.WithMappers(prblm.Is(errContract, func(error) *prblm.Problem { return sharedLocked })),
	)

	for _, requests := range []int{1, 200} {
		t.Run(strconv.Itoa(requests)+" requests", func(t *testing.T) {
			t.Parallel()

			var wg sync.WaitGroup

			for i := range requests {
				wg.Go(func() {
					rec := answer(t, h, errContract)

					var m map[string]any
					if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || m["type"] != "/v1/api/probs/contracts/locked" {
						t.Errorf("request %d: type %v (%v), want /v1/api/probs/contracts/locked", i, m["type"], err)
					}
				})
			}

			wg.Wait()

			if sharedLocked.Type != "contracts/locked" {
				t.Errorf("shared problem type = %q, want contracts/locked", sharedLocked.Type)
			}
		})
	}
}

// Design D2 failure mode: the fallback and panic paths go through the same
// completion, so a sub-type an enricher sets on them is resolved too.
func TestResolve_FallbackAndPanic(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(
		prblm.WithNamespace("/v1/api/probs"),
		prblm.WithEnrichers(func(_ context.Context, p *prblm.Problem) { p.Type = "server/unavailable" }),
	)

	tests := []struct {
		handler http.Handler
		name    string
	}{
		{name: "fallback", handler: h.Wrap(func(http.ResponseWriter, *http.Request) error { return errors.New("x") })},
		{name: "panic", handler: h.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serveRecorded(t, tt.handler, "")

			if got := typeMember(t, rec); rec.Code != http.StatusInternalServerError || got != "/v1/api/probs/server/unavailable" {
				t.Errorf("response %d with type %q, want 500 with /v1/api/probs/server/unavailable", rec.Code, got)
			}
		})
	}
}

// Design D2: completion finishes with finalize, after the enrichers and
// type resolution, so a problem that leaves them as about:blank without a
// title still gets the reason phrase of its status.
func TestComplete_FinalizeLast(t *testing.T) {
	t.Parallel()

	h := prblm.NewHandler(
		prblm.WithMappers(prblm.Status(errContract, http.StatusConflict)),
		prblm.WithEnrichers(func(_ context.Context, p *prblm.Problem) { p.Title = "" }),
	)

	if m := decodeMembers(t, answer(t, h, errContract).Body.Bytes()); m["title"] != "Conflict" {
		t.Errorf("members = %v, want the title Conflict", m)
	}
}

// problem-types / Scenario: Undocumented sub-type.
func TestResolve_Undocumented(t *testing.T) {
	t.Parallel()

	registered := prblm.WithTypes(prblm.DefineType("contracts/blacklisted-supplier", http.StatusConflict, "Supplier is blacklisted", ""))

	tests := []struct {
		name     string
		config   []prblm.Option
		given    string
		rendered string
		isNoted  bool
	}{
		{
			name:     "unknown sub-type",
			config:   []prblm.Option{registered},
			given:    "contracts/unknown",
			rendered: "/v1/api/probs/contracts/unknown",
			isNoted:  true,
		},
		{
			name:     "registered sub-type",
			config:   []prblm.Option{registered},
			given:    "contracts/blacklisted-supplier",
			rendered: "/v1/api/probs/contracts/blacklisted-supplier",
		},
		{
			name:     "discovery off",
			config:   []prblm.Option{registered, prblm.WithoutDiscovery()},
			given:    "contracts/unknown",
			rendered: "/v1/api/probs/contracts/unknown",
		},
		{name: "no types", given: "contracts/unknown", rendered: "/v1/api/probs/contracts/unknown"},
		{
			name:     "absolute type",
			config:   []prblm.Option{registered},
			given:    "https://example.com/probs/x",
			rendered: "https://example.com/probs/x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var r recorder

			opts := append([]prblm.Option{prblm.WithNamespace("/v1/api/probs"), conflictOfType(tt.given), prblm.OnError(r.hook)}, tt.config...)
			rec := answer(t, prblm.NewHandler(opts...), errContract)

			if got := typeMember(t, rec); got != tt.rendered {
				t.Errorf("type = %q, want %q", got, tt.rendered)
			}

			calls := r.recorded()
			isNoted := len(calls) == 2 && errors.Is(calls[0].err, prblm.ErrUndocumentedType)

			if isNoted != tt.isNoted || len(calls) != map[bool]int{true: 2, false: 1}[tt.isNoted] {
				t.Errorf("hook calls = %+v, want an ErrUndocumentedType note: %v", calls, tt.isNoted)
			}
		})
	}
}

// problem-types / Scenario: Type notes cost nothing without a hook.
//
//nolint:paralleltest // AllocsPerRun counts allocations process-wide; parallel tests would skew it.
func TestResolve_NoHookAllocs(t *testing.T) {
	tests := []struct {
		name   string
		given  string
		config []prblm.Option
	}{
		{name: "no namespace", given: "contracts/x"},
		{name: "invalid sub-type", given: "Bad Type", config: []prblm.Option{prblm.WithNamespace("/v1/api/probs")}},
		{
			name:  "undocumented sub-type",
			given: "contracts/unknown",
			config: []prblm.Option{
				prblm.WithNamespace("/v1/api/probs"),
				prblm.WithTypes(blacklistedSupplier),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolving := func(typ string) float64 {
				h := prblm.NewHandler(tt.config...)
				err := lockedOfType(typ)
				ctx := t.Context()

				return testing.AllocsPerRun(100, func() { _ = h.Resolve(ctx, err) })
			}

			if got, blank := resolving(tt.given), resolving("about:blank"); got > blank {
				t.Errorf("resolving type %q allocates %v times, want no more than the %v of about:blank", tt.given, got, blank)
			}
		})
	}
}
