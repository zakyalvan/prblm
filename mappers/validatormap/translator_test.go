package validatormap_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/id"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/validatormap"
)

// translated returns English and Indonesian translators and the error of
// validating {"age":0} with a validator that has "min" translated in both.
func translated(t *testing.T) (english, indonesian ut.Translator, err error) {
	t.Helper()

	uni := ut.New(en.New(), en.New(), id.New())
	english, _ = uni.GetTranslator("en")
	indonesian, _ = uni.GetTranslator("id")

	v := newValidator()

	for trans, text := range map[ut.Translator]string{english: "{0} is too small", indonesian: "{0} terlalu kecil"} {
		regErr := v.RegisterTranslation("min", trans,
			func(trans ut.Translator) error { return trans.Add("min", text, false) },
			func(trans ut.Translator, fe validator.FieldError) string {
				s, _ := trans.T("min", fe.Field())

				return s
			},
		)
		if regErr != nil {
			t.Fatalf("RegisterTranslation() error = %v", regErr)
		}
	}

	var req struct {
		Age int `json:"age" validate:"min=1"`
	}

	_ = json.Unmarshal([]byte(`{"age":0}`), &req)

	return english, indonesian, v.Struct(req)
}

// recordPanics returns an OnError hook that records into *dst the
// *prblm.PanicError in the cause of the problem being sent, if any.
// Recovered translator panics never reach the client, only the hook.
func recordPanics(dst *[]*prblm.PanicError) func(context.Context, error, *prblm.Problem) {
	return func(_ context.Context, _ error, p *prblm.Problem) {
		if pe, ok := errors.AsType[*prblm.PanicError](p); ok {
			*dst = append(*dst, pe)
		}
	}
}

func TestMapper_Translator(t *testing.T) {
	t.Parallel()

	english, indonesian, err := translated(t)

	// Scenario: Translator choices.
	tests := []struct {
		name      string
		want      string
		opts      []validatormap.Option
		wantPanic bool
	}{
		{
			name: "indonesian",
			opts: []validatormap.Option{validatormap.WithTranslator(func(context.Context) ut.Translator { return indonesian })},
			want: "age terlalu kecil",
		},
		{
			name: "english",
			opts: []validatormap.Option{validatormap.WithTranslator(func(context.Context) ut.Translator { return english })},
			want: "age is too small",
		},
		{
			name: "returns nil",
			opts: []validatormap.Option{validatormap.WithTranslator(func(context.Context) ut.Translator { return nil })},
			want: "failed validation: min",
		},
		{
			name:      "panics",
			opts:      []validatormap.Option{validatormap.WithTranslator(func(context.Context) ut.Translator { panic("no locale") })},
			want:      "failed validation: min",
			wantPanic: true,
		},
		{name: "nil function", opts: []validatormap.Option{validatormap.WithTranslator(nil)}, want: "failed validation: min"},
		{name: "no translator function", want: "failed validation: min"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var panics []*prblm.PanicError

			h := prblm.NewHandler(prblm.WithMappers(validatormap.Mapper(tt.opts...)), prblm.OnError(recordPanics(&panics)))
			got := serve(t, h, "", err)

			if got.status != 422 {
				t.Fatalf("status = %d, want 422", got.status)
			}

			if gotPanic := len(panics) == 1 && panics[0].Value == "no locale"; gotPanic != tt.wantPanic {
				t.Errorf("hook found panics %v, want the translator panic = %v", panics, tt.wantPanic)
			}

			want := `[{"detail":"` + tt.want + `","pointer":"#/age"}]`
			if e := errorsMember(t, got.body); e != want {
				t.Errorf("errors = %s, want %s", e, want)
			}
		})
	}
}

func TestMapper_TranslatorContext(t *testing.T) {
	t.Parallel()

	english, indonesian, err := translated(t)

	m := validatormap.Mapper(validatormap.WithTranslator(func(ctx context.Context) ut.Translator {
		if ctx.Value(localeKey{}) == "id" {
			return indonesian
		}

		return english
	}))

	for locale, want := range map[string]string{"id": "age terlalu kecil", "en": "age is too small"} {
		ctx := context.WithValue(t.Context(), localeKey{}, locale)

		p := m.MapError(ctx, err)
		if p == nil {
			t.Fatalf("%s: MapError() = nil, want a problem", locale)
		}

		b, marshalErr := json.Marshal(p)
		if marshalErr != nil {
			t.Fatalf("%s: Marshal() error = %v", locale, marshalErr)
		}

		if !strings.Contains(string(b), want) {
			t.Errorf("%s: problem %s does not contain %q", locale, b, want)
		}
	}
}

// TestMapper_TranslatorGaps covers translations that cannot be used: the
// translator has no translation for the tag, or the translation panics.
// Either way the default message is used, so the validator's own error text,
// which names the Go struct, never reaches the client.
func TestMapper_TranslatorGaps(t *testing.T) {
	t.Parallel()

	uni := ut.New(en.New(), en.New())
	english, _ := uni.GetTranslator("en")

	v := newValidator()

	err := v.RegisterTranslation("required", english,
		func(trans ut.Translator) error { return trans.Add("required", "{0} is required", false) },
		func(ut.Translator, validator.FieldError) string { panic("translation failed") },
	)
	if err != nil {
		t.Fatalf("RegisterTranslation() error = %v", err)
	}

	type SignupRequest struct {
		Name string `json:"name" validate:"required"`
		Age  int    `json:"age" validate:"min=1"`
	}

	var panics []*prblm.PanicError

	m := validatormap.Mapper(validatormap.WithTranslator(func(context.Context) ut.Translator { return english }))
	got := serve(t, prblm.NewHandler(prblm.WithMappers(m), prblm.OnError(recordPanics(&panics))), "", v.Struct(SignupRequest{}))

	if len(panics) != 1 || panics[0].Value != "translation failed" || len(panics[0].Stack) == 0 {
		t.Errorf("hook found panics %v, want the translation panic with its stack", panics)
	}

	want := `[{"detail":"failed validation: required","pointer":"#/name"},` +
		`{"detail":"failed validation: min","pointer":"#/age"}]`
	if e := errorsMember(t, got.body); e != want {
		t.Errorf("errors = %s, want %s", e, want)
	}

	if strings.Contains(got.body, "SignupRequest") {
		t.Errorf("body %q names the Go struct", got.body)
	}
}

// TestMapper_EndToEnd serves real HTTP requests, in JSON and XML, with and
// without translation, from a server with the mapper registered.
func TestMapper_EndToEnd(t *testing.T) {
	t.Parallel()

	_, indonesian, validationErr := translated(t)

	h := prblm.NewHandler(prblm.WithMappers(validatormap.Mapper(
		validatormap.WithTranslator(func(ctx context.Context) ut.Translator {
			if ctx.Value(localeKey{}) == "id" {
				return indonesian
			}

			return nil
		}),
	)))

	mux := http.NewServeMux()
	mux.Handle("POST /v1/signups", h.Wrap(func(http.ResponseWriter, *http.Request) error {
		return validationErr
	}))

	// A middleware stores the locale from Accept-Language.
	locale := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Language") == "id" {
			r = r.WithContext(context.WithValue(r.Context(), localeKey{}, "id"))
		}

		mux.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(locale)
	t.Cleanup(srv.Close)

	tests := []struct {
		accept   string
		language string
		want     string
	}{
		{accept: "application/problem+json", want: `"detail":"failed validation: min","pointer":"#/age"`},
		{accept: "application/problem+json", language: "id", want: `"detail":"age terlalu kecil","pointer":"#/age"`},
		{accept: "application/problem+xml", want: `<detail>failed validation: min</detail><pointer>#/age</pointer>`},
		{accept: "application/problem+xml", language: "id", want: `<detail>age terlalu kecil</detail><pointer>#/age</pointer>`},
	}

	for _, tt := range tests {
		t.Run(tt.accept+" "+tt.language, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/v1/signups", http.NoBody)
			if err != nil {
				t.Fatal(err)
			}

			req.Header.Set("Accept", tt.accept)
			req.Header.Set("Accept-Language", tt.language)

			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}

			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()

			if err != nil {
				t.Fatal(err)
			}

			if res.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422", res.StatusCode)
			}

			if ct := res.Header.Get("Content-Type"); ct != tt.accept {
				t.Errorf("Content-Type = %q, want %q", ct, tt.accept)
			}

			if !strings.Contains(string(body), tt.want) {
				t.Errorf("body %q does not contain %q", body, tt.want)
			}
		})
	}
}
