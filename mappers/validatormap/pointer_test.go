package validatormap

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
)

// jsonValidator returns a validator that reports JSON tag names.
func jsonValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
	v.RegisterTagNameFunc(JSONName)

	return v
}

// fieldErrors returns the field errors of validating s with v.
func fieldErrors(t *testing.T, v *validator.Validate, s any) validator.ValidationErrors {
	t.Helper()

	ves, ok := errors.AsType[validator.ValidationErrors](v.Struct(s))
	if !ok {
		t.Fatalf("Struct(%T) did not fail with ValidationErrors", s)
	}

	return ves
}

type signup struct {
	Profile struct {
		Color string `json:"color" validate:"oneof=red green blue"`
	} `json:"profile"`
	Age int `json:"age" validate:"min=1"`
}

type item struct {
	Name string   `json:"name" validate:"required"`
	Tags []string `json:"tags" validate:"dive,max=3"`
}

type order struct {
	Items []item `json:"items" validate:"dive"`
}

type settings struct {
	Meta map[string]string `json:"meta" validate:"dive,required"`
}

func TestPointer(t *testing.T) {
	t.Parallel()

	items := make([]item, 13)
	for i := range items {
		items[i] = item{Name: "n"}
	}

	items[0].Name = ""
	items[12].Tags = []string{"a", "b", "c", "long"}

	tests := []struct {
		value any
		name  string
		want  []string
	}{
		{name: "fields", value: signup{}, want: []string{"#/profile/color", "#/age"}},
		{name: "slices", value: order{Items: items}, want: []string{"#/items/0/name", "#/items/12/tags/3"}},
		{
			name:  "map keys",
			value: settings{Meta: map[string]string{"region": "", "a/b": "", "x~y": "", "café": ""}},
			want:  []string{"#/meta/a~1b", "#/meta/caf%C3%A9", "#/meta/region", "#/meta/x~0y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []string

			ves := fieldErrors(t, jsonValidator(), tt.value)
			jsonNames := usesJSONNames(ves)

			for _, fe := range ves {
				p, f := location(fe, jsonNames)
				if f != "" {
					t.Errorf("location(%q) field = %q, want a pointer", fe.Namespace(), f)
				}

				got = append(got, p)
			}

			// Map iteration order is random, so compare as sets.
			if !sameElements(got, tt.want) {
				t.Errorf("pointers = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("anonymous struct", func(t *testing.T) {
		t.Parallel()

		// The validator reports no root segment for an anonymous struct.
		var req struct {
			Inner struct {
				N int `json:"n" validate:"min=1"`
			} `json:"inner"`
			Age int `json:"age" validate:"min=1"`
		}

		var got []string

		ves := fieldErrors(t, jsonValidator(), req)
		jsonNames := usesJSONNames(ves)

		for _, fe := range ves {
			p, _ := location(fe, jsonNames)
			got = append(got, p)
		}

		if want := []string{"#/inner/n", "#/age"}; !sameElements(got, want) {
			t.Errorf("pointers = %q, want %q", got, want)
		}
	})

	t.Run("map key with delimiters", func(t *testing.T) {
		t.Parallel()

		// The validator writes a map key into the namespace unescaped, so the
		// key "x].admin[0" gives the namespace of the path meta, x, admin, 0,
		// name. The two can't be told apart, and the pointer follows the
		// path: a known limit, documented in the package documentation.
		type member struct {
			Name string `json:"name" validate:"required"`
		}

		var cfg struct {
			Meta map[string]member `json:"meta" validate:"dive"`
		}

		cfg.Meta = map[string]member{"x].admin[0": {}}

		ves := fieldErrors(t, jsonValidator(), cfg)

		if p, _ := location(ves[0], usesJSONNames(ves)); p != "#/meta/x/admin/0/name" {
			t.Errorf("location(%q) = %q, want %q", ves[0].Namespace(), p, "#/meta/x/admin/0/name")
		}
	})

	t.Run("single variable", func(t *testing.T) {
		t.Parallel()

		ves, ok := errors.AsType[validator.ValidationErrors](jsonValidator().Var(0, "min=1"))
		if !ok {
			t.Fatal("Var() did not fail with ValidationErrors")
		}

		if p, f := location(ves[0], usesJSONNames(ves)); p != "#" || f != "" {
			t.Errorf("location() = (%q, %q), want (\"#\", \"\")", p, f)
		}
	})
}

func TestPointer_Namespaces(t *testing.T) {
	t.Parallel()

	// Scenario: Pointer building, as namespaces.
	tests := []struct {
		namespace string
		want      string
	}{
		{namespace: "Signup.age", want: "#/age"},
		{namespace: "Signup.profile.color", want: "#/profile/color"},
		{namespace: "Order.items[0].name", want: "#/items/0/name"},
		{namespace: "Order.items[12].tags[3]", want: "#/items/12/tags/3"},
		{namespace: "Config.meta[region]", want: "#/meta/region"},
		{namespace: "Config.meta[a/b]", want: "#/meta/a~1b"},
		{namespace: "Config.meta[x~y]", want: "#/meta/x~0y"},
		{namespace: "Config.meta[café]", want: "#/meta/caf%C3%A9"},
		{namespace: "", want: "#"},
		{namespace: "Signup", want: "#"},
		{namespace: "[2]", want: "#/2"},
		{namespace: "[2].name", want: "#/2/name"},
		{namespace: "Config.meta[a.b]", want: "#/meta/a.b"},
		{namespace: "Config.meta[a[1]]", want: "#/meta/a%5B1%5D"},
		{namespace: "Config.meta[50%]", want: "#/meta/50%25"},
		{namespace: "Config.meta[a b]", want: "#/meta/a%20b"},
		{namespace: "Config.meta[]", want: "#/meta/"},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			t.Parallel()

			if got := pointerOf(relative(tt.namespace, true)); got != tt.want {
				t.Errorf("pointerOf(%q) = %q, want %q", tt.namespace, got, tt.want)
			}
		})
	}
}

func TestField(t *testing.T) {
	t.Parallel()

	type signupNoTags struct {
		VesselName string `validate:"required"`
	}

	type orderNoTags struct {
		Items []struct {
			Name string `validate:"required"`
		} `validate:"dive"`
	}

	// Scenario: No tag name function.
	tests := []struct {
		value any
		name  string
		want  string
	}{
		{name: "field", value: signupNoTags{}, want: "VesselName"},
		{name: "nested", value: orderNoTags{Items: make([]struct {
			Name string `validate:"required"`
		}, 1)}, want: "Items[0].Name"},
	}

	var anonymous struct {
		VesselName string `validate:"required"`
	}

	tests = append(tests, struct {
		value any
		name  string
		want  string
	}{name: "anonymous struct", value: anonymous, want: "VesselName"})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ves := fieldErrors(t, validator.New(), tt.value)

			p, f := location(ves[0], usesJSONNames(ves))
			if p != "" || f != tt.want {
				t.Errorf("location() = (%q, %q), want (\"\", %q)", p, f, tt.want)
			}
		})
	}
}

func FuzzPointer(f *testing.F) {
	for _, s := range []string{"", "Signup.age", "Order.items[12].tags[3]", "Config.meta[a/b]", "[", "]", "a[", "a]b[c", "a..b", "Config.meta[café]"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, namespace string) {
		got := pointerOf(relative(namespace, true))

		if !strings.HasPrefix(got, "#") {
			t.Errorf("pointer(%q) = %q, want a fragment", namespace, got)
		}

		if !utf8.ValidString(got) {
			t.Errorf("pointer(%q) = %q, not valid UTF-8", namespace, got)
		}

		for _, r := range got {
			if r > 0x7e || r < 0x21 {
				t.Errorf("pointer(%q) = %q, contains unescaped %q", namespace, got, r)
			}
		}
	})
}

// sameElements reports whether a and b hold the same strings, in any order.
func sameElements(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)

	return slices.Equal(a, b)
}
