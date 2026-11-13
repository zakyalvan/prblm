// Package validatormap maps go-playground/validator errors
// ([validator.ValidationErrors]) to RFC 9457 validation problems.
//
// It is its own module, so the validator never becomes a dependency of the
// core module. The mapper does nothing until it is registered, so importing
// the package changes no behavior:
//
//	h := prblm.NewHandler(prblm.WithMappers(validatormap.Mapper()))
//
// # The validation problem
//
// Registered, the mapper turns non-empty validation errors in the chain into
// a problem with status 422, the detail "The request is not valid." and an
// errors extension member, the shape of the RFC 9457 example: one object per
// failed field, in the validator's order, with a detail and a JSON Pointer
// to the field in the request body.
//
//	{
//	  "title": "Unprocessable Entity",
//	  "status": 422,
//	  "detail": "The request is not valid.",
//	  "errors": [
//	    {"detail": "failed validation: min", "pointer": "#/age"},
//	    {"detail": "failed validation: oneof", "pointer": "#/profile/color"}
//	  ]
//	}
//
// The status is 422 because the body was well-formed but its content is
// invalid; [WithStatus] sets another 4xx status, such as 400. [WithType] and
// [WithTitle] set the type and title. In XML, the errors member is a list of
// <i> elements.
//
// The detail of an entry is "failed validation: <tag>". The rejected value,
// the tag's parameter and the Go struct and type names never appear, since
// they can carry personal data or internal names. [WithTranslator] replaces
// the detail with a translated message.
//
// Invalid use of the validator, [*validator.InvalidValidationError] (for
// example validating nil), is a server fault: it is left to the handler's
// safe 500 fallback, as are empty validation errors.
//
// # Pointers need JSON names
//
// The validator reports Go field names unless it has a tag name function.
// Register [JSONName], which names fields as encoding/json does, on a
// validator that omits blank names, so the mapper can build pointers:
//
//	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
//	v.RegisterTagNameFunc(validatormap.JSONName)
//
// Both are needed for embedded structs. encoding/json promotes the fields of
// an embedded struct without a json tag to the enclosing object, so its
// field Name is at "#/name"; [JSONName] returns "" for the embedded struct,
// and WithTagNameFuncBlankOmit makes the validator leave that "" out of the
// namespace instead of using the Go type name.
//
// A pointer is an RFC 6901 JSON Pointer in URI fragment form: slice indexes
// and map keys are tokens, "~" and "/" in a key are escaped as "~0" and
// "~1", and other characters are percent-encoded ("#/meta/caf%C3%A9").
// Without JSON names, a pointer would be wrong, so each entry instead has a
// field member: the Go namespace without the root struct
// ("Items[0].Name"). The validator has no getter for its tag name function,
// so the mapper decides once per problem: when any entry's JSON namespace
// differs from its Go namespace, every entry has a pointer, otherwise every
// entry has a field. A problem never mixes the two. When every failed
// field's JSON name equals its Go name, the entries have fields, which
// still name the fields correctly. Validate named struct types: for an
// anonymous struct the validator reports no root name, which the mapper can
// only partly tell apart from a named one.
//
// # Translation
//
// [WithTranslator] picks a universal-translator Translator per request, for
// example from a locale that a middleware stored in the context. Register
// translations on the same validator that validates the requests (see
// validator's translations packages). A tag without a translation, a
// translator function that returns nil, and a translation or function that
// panics all fall back to the default detail, so a missing translation never
// turns a 422 into a 500. A recovered panic becomes the cause of the
// problem, as a [*prblm.PanicError] with the panic value and stack (joined
// with [errors.Join] when there are several). The cause never reaches the
// client, but the handler's OnError hook receives the problem, so it can
// find the panic:
//
//	prblm.OnError(func(ctx context.Context, err error, p *prblm.Problem) {
//		if pe, ok := errors.AsType[*prblm.PanicError](p); ok {
//			slog.ErrorContext(ctx, "translator panicked", "panic", pe.Value)
//		}
//	})
//
// There is no limit on the number of entries other than what the validator
// returns, which is at most one per field.
package validatormap
