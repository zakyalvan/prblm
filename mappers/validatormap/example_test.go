package validatormap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"

	"github.com/go-playground/locales/en"
	"github.com/go-playground/locales/id"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"

	"github.com/kartaladev/prblm"
	"github.com/kartaladev/prblm/mappers/validatormap"
)

type vesselRequest struct {
	Name string `json:"name" validate:"required"`
	Crew int    `json:"crew" validate:"min=1"`
}

// jsonNames makes v report JSON field names, so pointers can be built.
func jsonNames(v *validator.Validate) {
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}

		return name
	})
}

func ExampleMapper() {
	v := validator.New(validator.WithRequiredStructEnabled())
	jsonNames(v)

	h := prblm.NewHandler(prblm.WithMappers(validatormap.Mapper()))

	handler := h.Wrap(func(_ http.ResponseWriter, r *http.Request) error {
		var req vesselRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return err
		}

		return v.StructCtx(r.Context(), req)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/vessels",
		strings.NewReader(`{"name":"","crew":0}`))
	handler.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Body.String())
	// Output:
	// 422
	// {"title":"Unprocessable Entity","status":422,"detail":"The request is not valid.","instance":"/v1/vessels","errors":[{"detail":"failed validation: required","pointer":"#/name"},{"detail":"failed validation: min","pointer":"#/crew"}]}
}

// localeKey is the context key under which a middleware stores the
// request's locale.
type localeKey struct{}

func ExampleWithTranslator() {
	uni := ut.New(en.New(), en.New(), id.New())
	english, _ := uni.GetTranslator("en")
	indonesian, _ := uni.GetTranslator("id")

	v := validator.New(validator.WithRequiredStructEnabled())
	jsonNames(v)

	// Translations are registered on the validator that validates requests.
	for trans, text := range map[ut.Translator]string{english: "{0} is required", indonesian: "{0} wajib diisi"} {
		_ = v.RegisterTranslation("required", trans,
			func(trans ut.Translator) error { return trans.Add("required", text, false) },
			func(trans ut.Translator, fe validator.FieldError) string {
				s, _ := trans.T("required", fe.Field())
				return s
			},
		)
	}

	h := prblm.NewHandler(prblm.WithMappers(validatormap.Mapper(
		validatormap.WithTranslator(func(ctx context.Context) ut.Translator {
			if ctx.Value(localeKey{}) == "id" {
				return indonesian
			}

			return english
		}),
	)))

	handler := h.Wrap(func(_ http.ResponseWriter, r *http.Request) error {
		return v.StructCtx(r.Context(), vesselRequest{Crew: 3})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/vessels", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), localeKey{}, "id"))
	handler.ServeHTTP(rec, req)

	fmt.Println(rec.Body.String())
	// Output:
	// {"title":"Unprocessable Entity","status":422,"detail":"The request is not valid.","instance":"/v1/vessels","errors":[{"detail":"name wajib diisi","pointer":"#/name"}]}
}
