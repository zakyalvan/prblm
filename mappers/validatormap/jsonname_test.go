package validatormap_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/go-playground/validator/v10"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/validatormap"
)

// ID is a named non-struct type, which encoding/json does not flatten when
// embedded.
type ID string

type names struct {
	Base
	ID
	Email  string `json:"email,omitempty"`
	Plain  string
	Opts   string `json:",omitempty"`
	Secret string `json:"-"`
	Dash   string `json:"-,"` //nolint:staticcheck // encoding/json names this field "-", which JSONName must follow.
	Tagged Base   `json:"base"`
}

func TestJSONName(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeFor[names]()

	// Scenario: JSON names.
	tests := []struct {
		field string
		want  string
	}{
		{field: "Email", want: "email"},
		{field: "Opts", want: "Opts"},
		{field: "Plain", want: "Plain"},
		{field: "Secret", want: ""},
		{field: "Dash", want: "-"},
		{field: "Base", want: ""},
		{field: "Tagged", want: "base"},
		{field: "ID", want: "ID"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			t.Parallel()

			fld, ok := typ.FieldByName(tt.field)
			if !ok {
				t.Fatalf("no field %s", tt.field)
			}

			if got := validatormap.JSONName(fld); got != tt.want {
				t.Errorf("JSONName(%s) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

// Base is a struct to embed.
type Base struct {
	Name string `json:"name" validate:"required"`
}

type embeddedOrder struct {
	Base
}

type embeddedPointerOrder struct {
	*Base
}

type taggedOrder struct {
	Base `json:"base"`
}

// TestMapper_EmbeddedStruct covers the scenario "Embedded structs": with
// JSONName on a validator that omits blank names, a pointer names the
// location encoding/json decodes the field from.
func TestMapper_EmbeddedStruct(t *testing.T) {
	t.Parallel()

	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
	v.RegisterTagNameFunc(validatormap.JSONName)

	tests := []struct {
		value any
		name  string
		want  string
	}{
		{name: "embedded", value: embeddedOrder{}, want: "#/name"},
		{name: "embedded pointer", value: embeddedPointerOrder{Base: &Base{}}, want: "#/name"},
		{name: "embedded with a json tag", value: taggedOrder{}, want: "#/base/name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, prblm.NewHandler(prblm.WithMappers(validatormap.Mapper())), "", v.Struct(tt.value))

			if got.status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", got.status)
			}

			want := `[{"detail":"failed validation: required","pointer":"` + tt.want + `"}]`
			if e := errorsMember(t, got.body); e != want {
				t.Errorf("errors = %s, want %s", e, want)
			}
		})
	}
}

type account struct {
	ID   string `json:"ID" validate:"required"`
	Name string `json:"name" validate:"required"`
}

// TestMapper_NamesEqualGoNames covers the scenario "JSON names equal to Go
// names": a problem never mixes pointer and field entries.
func TestMapper_NamesEqualGoNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value account
		name  string
		want  string
	}{
		{
			name: "ID and Name",
			want: `[{"detail":"failed validation: required","pointer":"#/ID"},` +
				`{"detail":"failed validation: required","pointer":"#/name"}]`,
		},
		{
			name:  "ID only",
			value: account{Name: "Alice"},
			want:  `[{"detail":"failed validation: required","field":"ID"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, prblm.NewHandler(prblm.WithMappers(validatormap.Mapper())), "", newValidator().Struct(tt.value))

			if e := errorsMember(t, got.body); e != tt.want {
				t.Errorf("errors = %s, want %s", e, tt.want)
			}
		})
	}
}
