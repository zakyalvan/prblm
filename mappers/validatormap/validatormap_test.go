package validatormap_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/validatormap"
)

// signupRequest is a request body with an age and a nested color, in that
// order, which is the order the validator reports them in.
type signupRequest struct {
	Age     int `json:"age" validate:"min=1"`
	Profile struct {
		Color string `json:"color" validate:"oneof=red green blue"`
	} `json:"profile"`
}

// newValidator returns a validator that reports JSON tag names.
func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
	v.RegisterTagNameFunc(validatormap.JSONName)

	return v
}

// validate decodes body into a new T and validates it, returning the
// validation error.
func validate[T any](t *testing.T, body string) error {
	t.Helper()

	var req T
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", body, err)
	}

	err := newValidator().Struct(req)
	if err == nil {
		t.Fatalf("Struct(%s) passed, want a validation error", body)
	}

	return err
}

// response is what a client sees of a problem response.
type response struct {
	problem     *prblm.Problem
	contentType string
	body        string
	status      int
}

// serve answers POST /v1/signups with err through h, with the given Accept
// header, and returns the response the client receives.
func serve(t *testing.T, h *prblm.Handler, accept string, err error) response {
	t.Helper()

	handler := h.Wrap(func(http.ResponseWriter, *http.Request) error {
		return err
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/signups", http.NoBody)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	res := rec.Result()
	body := rec.Body.String()

	p, parseErr := prblm.Parse(res)
	if parseErr != nil {
		t.Fatalf("Parse() error = %v, body %q", parseErr, body)
	}

	return response{problem: p, contentType: res.Header.Get("Content-Type"), body: body, status: res.StatusCode}
}

// errorsMember returns the compact JSON of the errors member of a JSON
// problem body, or "" when it has none.
func errorsMember(t *testing.T, body string) string {
	t.Helper()

	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("Unmarshal(%q) error = %v", body, err)
	}

	raw, ok := doc["errors"]
	if !ok {
		return ""
	}

	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}

	return b.String()
}

func TestMapper_Validation(t *testing.T) {
	t.Parallel()

	const (
		ageOnly = `[{"detail":"failed validation: min","pointer":"#/age"}]`
		both    = `[{"detail":"failed validation: min","pointer":"#/age"},` +
			`{"detail":"failed validation: oneof","pointer":"#/profile/color"}]`
	)

	invalidAge := validate[signupRequest](t, `{"age":0,"profile":{"color":"red"}}`)

	tests := []struct {
		err        error
		name       string
		wantErrors string
		mappers    []prblm.Mapper
		wantStatus int
	}{
		// Scenario: Registered or not.
		{name: "registered", mappers: []prblm.Mapper{validatormap.Mapper()}, err: invalidAge, wantStatus: 422, wantErrors: ageOnly},
		{name: "not registered", err: invalidAge, wantStatus: http.StatusInternalServerError},
		// Scenario: Invalid fields.
		{
			name:       "two fields",
			mappers:    []prblm.Mapper{validatormap.Mapper()},
			err:        validate[signupRequest](t, `{"age":0,"profile":{"color":"pink"}}`),
			wantStatus: 422,
			wantErrors: both,
		},
		// Scenario: Wrapped and empty validation errors.
		{
			name:       "wrapped",
			mappers:    []prblm.Mapper{validatormap.Mapper()},
			err:        fmt.Errorf("decode: %w", invalidAge),
			wantStatus: 422,
			wantErrors: ageOnly,
		},
		{
			name:       "joined",
			mappers:    []prblm.Mapper{validatormap.Mapper()},
			err:        errors.Join(io.EOF, invalidAge),
			wantStatus: 422,
			wantErrors: ageOnly,
		},
		{
			name:       "empty",
			mappers:    []prblm.Mapper{validatormap.Mapper()},
			err:        validator.ValidationErrors{},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, prblm.NewHandler(prblm.WithMappers(tt.mappers...)), "", tt.err)

			if got.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", got.status, tt.wantStatus)
			}

			if got.contentType != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", got.contentType)
			}

			if e := errorsMember(t, got.body); e != tt.wantErrors {
				t.Errorf("errors = %s, want %s", e, tt.wantErrors)
			}

			if tt.wantStatus == 422 && got.problem.Detail != "The request is not valid." {
				t.Errorf("detail = %q, want %q", got.problem.Detail, "The request is not valid.")
			}
		})
	}
}

// SignupRequest has a password that must be at least 12 characters long.
type SignupRequest struct {
	Password string `json:"password" validate:"min=12"`
}

func TestMapper_NoLeak(t *testing.T) {
	t.Parallel()

	err := validate[SignupRequest](t, `{"password":"hunter2"}`)

	// Without a tag name function too, where the field fallback applies.
	untagged := validator.New().Struct(SignupRequest{Password: "hunter2"})

	for _, e := range []error{err, untagged} {
		for _, accept := range []string{"application/problem+json", "application/problem+xml"} {
			got := serve(t, prblm.NewHandler(prblm.WithMappers(validatormap.Mapper())), accept, e)

			if got.status != 422 {
				t.Errorf("%s: status = %d, want 422", accept, got.status)
			}

			for _, forbidden := range []string{"hunter2", "SignupRequest", "12"} {
				if strings.Contains(got.body, forbidden) {
					t.Errorf("%s: body %q contains %q", accept, got.body, forbidden)
				}
			}
		}
	}
}

func TestMapper_Options(t *testing.T) {
	t.Parallel()

	const customType = "https://example.com/probs/validation"

	// Scenario: Configured problem.
	tests := []struct {
		name      string
		wantType  string
		wantTitle string
		opts      []validatormap.Option
		want      int
	}{
		{name: "no options", want: 422, wantType: "about:blank", wantTitle: "Unprocessable Entity"},
		{
			name:      "status 400",
			opts:      []validatormap.Option{validatormap.WithStatus(http.StatusBadRequest)},
			want:      http.StatusBadRequest,
			wantType:  "about:blank",
			wantTitle: "Bad Request",
		},
		{
			name:      "type and title",
			opts:      []validatormap.Option{validatormap.WithType(customType), validatormap.WithTitle("Your request is not valid.")},
			want:      422,
			wantType:  customType,
			wantTitle: "Your request is not valid.",
		},
		{name: "nil option", opts: []validatormap.Option{nil}, want: 422, wantType: "about:blank", wantTitle: "Unprocessable Entity"},
	}

	err := validate[signupRequest](t, `{"age":0,"profile":{"color":"red"}}`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := serve(t, prblm.NewHandler(prblm.WithMappers(validatormap.Mapper(tt.opts...))), "", err)

			if got.status != tt.want || got.problem.Status != tt.want {
				t.Errorf("status = %d (document %d), want %d", got.status, got.problem.Status, tt.want)
			}

			if got.problem.EffectiveType() != tt.wantType {
				t.Errorf("type = %q, want %q", got.problem.EffectiveType(), tt.wantType)
			}

			if got.problem.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", got.problem.Title, tt.wantTitle)
			}
		})
	}
}

func TestMapper_OptionsInvalidStatus(t *testing.T) {
	t.Parallel()

	// Scenario: Invalid status.
	for _, status := range []int{200, 500, 0, 399, 600} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("no panic, want one naming WithStatus")
				}

				if msg := fmt.Sprint(r); !strings.Contains(msg, "WithStatus") {
					t.Errorf("panic %q does not name WithStatus", msg)
				}
			}()

			validatormap.Mapper(validatormap.WithStatus(status))
		})
	}
}

// myError is a consumer error type, used to store a typed nil in an error.
type myError struct{}

func (*myError) Error() string { return "my error" }

func TestMapper_Degenerate(t *testing.T) {
	t.Parallel()

	var typedNil *myError

	invalid := validator.New().Struct(nil)
	if _, ok := errors.AsType[*validator.InvalidValidationError](invalid); !ok {
		t.Fatalf("Struct(nil) error = %v, want *validator.InvalidValidationError", invalid)
	}

	tests := []struct {
		err  error
		name string
	}{
		{name: "nil", err: nil},
		{name: "invalid validation error", err: invalid},
		{name: "typed nil", err: typedNil},
		{name: "nil validation errors", err: validator.ValidationErrors(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if p := validatormap.Mapper().MapError(t.Context(), tt.err); p != nil {
				t.Errorf("MapError() = %+v, want no match", p)
			}
		})
	}
}

func TestMapper_XML(t *testing.T) {
	t.Parallel()

	err := validate[signupRequest](t, `{"age":0,"profile":{"color":"red"}}`)

	got := serve(t, prblm.NewHandler(prblm.WithMappers(validatormap.Mapper())), "application/problem+xml", err)

	if got.contentType != "application/problem+xml" {
		t.Errorf("Content-Type = %q, want application/problem+xml", got.contentType)
	}

	const want = `<errors><i><detail>failed validation: min</detail><pointer>#/age</pointer></i></errors>`
	if !strings.Contains(got.body, want) {
		t.Errorf("body %q does not contain %q", got.body, want)
	}
}
