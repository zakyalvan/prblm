package validatormap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"

	"github.com/kartaladev/prblm"
)

// entry is one failed field in the errors extension. It is encoded through
// its json tags in both JSON and XML. Exactly one of Pointer and Field is
// set.
type entry struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer,omitempty"`
	Field   string `json:"field,omitempty"`
}

// Option configures the mapper returned by [Mapper].
type Option func(*config)

// config holds the settings of a mapper.
type config struct {
	// translator picks the translator of a request, or is nil.
	translator func(ctx context.Context) ut.Translator
	typ        string
	title      string
	status     int
}

// WithStatus sets the status of validation problems, 422 by default. It
// must be a client error status, from 400 to 499: a validation failure is
// never the server's fault. WithStatus panics on any other status.
func WithStatus(code int) Option {
	if code < http.StatusBadRequest || code > 499 {
		panic(fmt.Sprintf("validatormap: WithStatus needs a status from 400 to 499, got %d", code))
	}

	return func(c *config) {
		c.status = code
	}
}

// WithType sets the type of validation problems, a URI reference. By
// default it is empty, which means about:blank.
func WithType(uri string) Option {
	return func(c *config) {
		c.typ = uri
	}
}

// WithTitle sets the title of validation problems. By default it is empty,
// and the handler uses the reason phrase of the status for about:blank
// problems.
func WithTitle(title string) Option {
	return func(c *config) {
		c.title = title
	}
}

// WithTranslator sets the function that picks the translator for a
// request, for example from the locale a middleware stored in ctx. With a
// translator, each entry's detail is the validator's translation of the
// failed tag. Without one, because fn is nil or returns nil, or because fn
// panics, the detail is "failed validation: <tag>".
func WithTranslator(fn func(ctx context.Context) ut.Translator) Option {
	return func(c *config) {
		c.translator = fn
	}
}

// Mapper returns a mapper for validation errors, configured by opts (see
// the package documentation for the problem it builds). Nil options are
// ignored.
//
// The options are applied once, here; the returned mapper does not change
// afterwards and is safe for concurrent use, as long as the translators it
// is given are.
func Mapper(opts ...Option) prblm.Mapper {
	cfg := config{status: http.StatusUnprocessableEntity}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	// The fields every validation problem shares, built once.
	fixed := []prblm.Field{
		prblm.WithType(cfg.typ),
		prblm.WithTitle(cfg.title),
		prblm.WithDetail("The request is not valid."),
	}

	return prblm.MapperFunc(func(ctx context.Context, err error) *prblm.Problem {
		ves, ok := errors.AsType[validator.ValidationErrors](err)
		if !ok || len(ves) == 0 {
			return nil
		}

		trans := cfg.translatorFor(ctx)

		entries := make([]entry, 0, len(ves))
		for _, fe := range ves {
			p, f := location(fe)
			entries = append(entries, entry{Detail: message(fe, trans), Pointer: p, Field: f})
		}

		// Clipping makes append copy, so concurrent calls
		// never share the backing array of fixed.
		return prblm.New(cfg.status, append(slices.Clip(fixed), prblm.WithExt("errors", entries))...)
	})
}

// translatorFor returns the translator for the request of ctx, or nil when
// there is none or the consumer's function panics. A panic is contained
// here rather than by the handler: the translation is an enhancement, and
// losing the whole validation problem to a 500 would be worse for clients.
func (c *config) translatorFor(ctx context.Context) (trans ut.Translator) {
	if c.translator == nil {
		return nil
	}

	defer func() {
		if recover() != nil {
			trans = nil
		}
	}()

	return c.translator(ctx)
}

// message returns the detail of an entry: the translation of fe by trans,
// or "failed validation: <tag>". It never uses the rejected value, the
// parameter or fe.Error(), which can carry them and the Go struct names.
func message(fe validator.FieldError, trans ut.Translator) string {
	if text, ok := translate(fe, trans); ok {
		return text
	}

	return "failed validation: " + fe.Tag()
}

// translate returns the translation of fe by trans, and false when there is
// no translator, the translator has no translation for the tag (the
// validator then falls back to fe.Error(), which names the Go struct), or
// the translation panics.
func translate(fe validator.FieldError, trans ut.Translator) (text string, ok bool) {
	if trans == nil {
		return "", false
	}

	defer func() {
		if recover() != nil {
			text, ok = "", false
		}
	}()

	text = fe.Translate(trans)

	return text, text != fe.Error()
}
