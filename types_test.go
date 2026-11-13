package prblm_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/kartaladev/prblm"
)

// problem-types / Scenario: Definition validated.
func TestDefineType_Validation(t *testing.T) {
	t.Parallel()

	type definition struct {
		subtype string
		title   string
		status  int
	}

	accepted := []definition{
		{subtype: "contracts/blacklisted-supplier", status: 409, title: "Supplier is blacklisted"},
		{subtype: "out-of-credit", status: 403, title: "Out of credit"},
		{subtype: "v2/limits/rate.exceeded", status: 429, title: "Rate exceeded"},
		{subtype: "x", status: 400, title: "X"},
		{subtype: "x", status: 599, title: "X"},
	}

	for _, d := range accepted {
		t.Run("accepted "+d.subtype+" "+strconv.Itoa(d.status), func(t *testing.T) {
			t.Parallel()

			typ := prblm.DefineType(d.subtype, d.status, d.title, "")

			if typ.Subtype() != d.subtype || typ.Status() != d.status || typ.Title() != d.title || typ.Description() != "" {
				t.Errorf("definition = %q %d %q %q, want %q %d %q and no description",
					typ.Subtype(), typ.Status(), typ.Title(), typ.Description(), d.subtype, d.status, d.title)
			}
		})
	}

	rejected := []struct {
		definition

		named string
	}{
		{subtype: "", status: 409, title: "X", named: `""`},
		{subtype: "Contracts/X", status: 409, title: "X", named: `"Contracts/X"`},
		{subtype: "/contracts/x", status: 409, title: "X", named: `"/contracts/x"`},
		{subtype: "https://kartala.id/x", status: 409, title: "X", named: `"https://kartala.id/x"`},
		{subtype: "contracts//x", status: 409, title: "X", named: `"contracts//x"`},
		{subtype: "contracts/../x", status: 409, title: "X", named: `"contracts/../x"`},
		{subtype: "x", status: 399, title: "X", named: "399"},
		{subtype: "x", status: 600, title: "X", named: "600"},
		{subtype: "x", status: 0, title: "X", named: "status: 0,"},
		{subtype: "x", status: 409, title: "", named: `title: ""`},
		{subtype: "x", status: 409, title: "   ", named: `title: "   "`},
	}

	for _, tt := range rejected {
		t.Run("rejected "+tt.named, func(t *testing.T) {
			t.Parallel()

			msg := mustPanic(t, func() { prblm.DefineType(tt.subtype, tt.status, tt.title, "") })

			if !strings.Contains(msg, "DefineType") || !strings.Contains(msg, tt.named) {
				t.Errorf("panic = %q, want a message naming DefineType and %s", msg, tt.named)
			}
		})
	}
}

// blacklistedSupplier is the definition of the problem-types scenarios.
var blacklistedSupplier = prblm.DefineType(
	"contracts/blacklisted-supplier",
	http.StatusConflict,
	"Supplier is blacklisted",
	"The supplier is on the procurement blacklist.",
)

// problem-types / Scenario: Problem from a definition.
func TestType_New(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields []prblm.Field
		json   string
	}{
		{
			name: "no fields",
			json: `{"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"instance":"/v1/contracts/7"}`,
		},
		{
			name:   "detail",
			fields: []prblm.Field{prblm.WithDetail("Supplier 42 is blacklisted.")},
			json:   `{"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"detail":"Supplier 42 is blacklisted.","instance":"/v1/contracts/7"}`,
		},
		{
			name:   "extension",
			fields: []prblm.Field{prblm.WithExt("supplier_id", 42)},
			json:   `{"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"instance":"/v1/contracts/7","supplier_id":42}`,
		},
		{
			name:   "nil field",
			fields: []prblm.Field{nil},
			json:   `{"type":"/v1/api/probs/contracts/blacklisted-supplier","title":"Supplier is blacklisted","status":409,"instance":"/v1/contracts/7"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), prblm.WithMappers(
				prblm.Is(errContract, func(error) *prblm.Problem { return blacklistedSupplier.New(tt.fields...) }),
			))

			rec := answer(t, h, errContract)

			if rec.Code != http.StatusConflict || rec.Body.String() != tt.json {
				t.Errorf("response %d %s, want 409 %s", rec.Code, rec.Body.String(), tt.json)
			}
		})
	}
}

// problem-types / Scenario: Each problem is a fresh copy.
func TestType_New_FreshCopy(t *testing.T) {
	t.Parallel()

	first := blacklistedSupplier.New()
	second := blacklistedSupplier.New()
	first.Detail = "a"

	if second.Detail != "" {
		t.Errorf("second detail = %q, want empty", second.Detail)
	}
}

// problem-types / Scenario: Nil definition.
func TestType_New_Nil(t *testing.T) {
	t.Parallel()

	const safeContract500 = `{"title":"Internal Server Error","status":500,"instance":"/v1/contracts/7"}`

	tests := []struct {
		name   string
		fields []prblm.Field
	}{
		{name: "no fields"},
		{name: "detail", fields: []prblm.Field{prblm.WithDetail("secret")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var undefined *prblm.Type

			h := prblm.NewHandler(prblm.WithNamespace("/v1/api/probs"), prblm.WithMappers(
				prblm.Is(errContract, func(error) *prblm.Problem { return undefined.New(tt.fields...) }),
			))

			rec := answer(t, h, errContract)

			if rec.Code != http.StatusInternalServerError || rec.Body.String() != safeContract500 {
				t.Errorf("response %d %s, want 500 %s", rec.Code, rec.Body.String(), safeContract500)
			}
		})
	}
}

// Design D3 failure mode for problem-types / Scenario: Nil definition: the
// nil receiver itself answers with the safe 500, so it neither panics nor
// sends a panic note to the hook, and a field such as a detail never
// reaches it.
func TestType_New_NilReceiver(t *testing.T) {
	t.Parallel()

	var undefined *prblm.Type

	p := undefined.New(prblm.WithDetail("secret"))

	if p.Status != http.StatusInternalServerError || p.Detail != "" || p.Type != "" {
		t.Errorf("problem = %+v, want a bare 500 without detail", *p)
	}
}

// problem-types / Scenario: Registration validated.
func TestWithTypes_Validation(t *testing.T) {
	t.Parallel()

	a := prblm.DefineType("contracts/blacklisted-supplier", http.StatusConflict, "Supplier is blacklisted", "")
	a2 := prblm.DefineType("contracts/blacklisted-supplier", http.StatusForbidden, "Another title", "")
	b := prblm.DefineType("out-of-credit", http.StatusForbidden, "Out of credit", "")
	ns := prblm.WithNamespace("/v1/api/probs")

	accepted := []struct {
		name string
		opts []prblm.Option
	}{
		{name: "namespace then types", opts: []prblm.Option{ns, prblm.WithTypes(a, b)}},
		{name: "types then namespace", opts: []prblm.Option{prblm.WithTypes(a), ns}},
		{name: "types in two options", opts: []prblm.Option{ns, prblm.WithTypes(a), prblm.WithTypes(b)}},
		{name: "a nil type", opts: []prblm.Option{ns, prblm.WithTypes(a, nil)}},
		{name: "no types", opts: []prblm.Option{ns, prblm.WithTypes()}},
		{name: "no types and no namespace", opts: []prblm.Option{prblm.WithTypes()}},
	}

	for _, tt := range accepted {
		t.Run("accepted "+tt.name, func(t *testing.T) {
			t.Parallel()

			prblm.NewHandler(tt.opts...)
		})
	}

	rejected := []struct {
		name  string
		opts  []prblm.Option
		named string
	}{
		{name: "no namespace", opts: []prblm.Option{prblm.WithTypes(a)}, named: "WithNamespace"},
		{name: "same type twice", opts: []prblm.Option{ns, prblm.WithTypes(a, a)}, named: `"contracts/blacklisted-supplier"`},
		{
			name:  "same sub-type in two options",
			opts:  []prblm.Option{ns, prblm.WithTypes(a), prblm.WithTypes(a2)},
			named: `"contracts/blacklisted-supplier"`,
		},
	}

	for _, tt := range rejected {
		t.Run("rejected "+tt.name, func(t *testing.T) {
			t.Parallel()

			msg := mustPanic(t, func() { prblm.NewHandler(tt.opts...) })

			if !strings.Contains(msg, "WithTypes") || !strings.Contains(msg, tt.named) {
				t.Errorf("panic = %q, want a message naming WithTypes and %s", msg, tt.named)
			}
		})
	}
}
