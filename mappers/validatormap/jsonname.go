package validatormap

import (
	"reflect"
	"strings"
)

// JSONName returns the name encoding/json gives the struct field fld, for
// use as the validator's tag name function, so that pointers name the
// location a field is decoded from:
//
//	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
//	v.RegisterTagNameFunc(validatormap.JSONName)
//
// The name is the one in the field's json tag, or the Go field name when the
// tag has none. It is "" for a field the tag excludes ("-"), and for an
// embedded struct, or pointer to struct, without a tag name, whose fields
// encoding/json promotes to the enclosing object. The validator drops a ""
// segment from namespaces only when created with
// validator.WithTagNameFuncBlankOmit; without that option, it uses the Go
// name instead, and the pointer to a field of an embedded struct then
// includes the name of the struct, a location the JSON document does not
// have.
func JSONName(fld reflect.StructField) string {
	tag := fld.Tag.Get("json")
	if tag == "-" {
		return ""
	}

	if name, _, _ := strings.Cut(tag, ","); name != "" {
		return name
	}

	if fld.Anonymous && isStruct(fld.Type) {
		return ""
	}

	return fld.Name
}

// isStruct reports whether t is a struct type or a pointer to one, the
// embedded types whose fields encoding/json promotes.
func isStruct(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t.Kind() == reflect.Struct
}
