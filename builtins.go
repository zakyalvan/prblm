package prblm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// builtins map the standard library errors a handler commonly returns while
// reading a request body. Each detail is a fixed sentence: offsets, field
// names and the error text never reach the client.
//
// The first match wins, so the order matters for an error with several
// causes: errors whose type identifies their source come before sentinels
// that could come from anywhere.
//
// A bare io.EOF is deliberately not mapped: it comes from any reader,
// including an upstream service closing early, and a 400 would blame the
// client for a server failure. It falls to the safe 500; a consumer that
// knows its io.EOF means an empty body maps it with its own mapper.
var builtins = []Mapper{
	As(func(*http.MaxBytesError) *Problem {
		return &Problem{Status: http.StatusRequestEntityTooLarge, Detail: "The request body is too large."}
	}),
	As(func(*json.UnmarshalTypeError) *Problem {
		return &Problem{Status: http.StatusUnprocessableEntity, Detail: "The request body has a value of the wrong type."}
	}),
	As(func(*json.SyntaxError) *Problem {
		return &Problem{Status: http.StatusBadRequest, Detail: "The request body is not valid JSON."}
	}),
	Is(io.ErrUnexpectedEOF, func(error) *Problem {
		return &Problem{Status: http.StatusBadRequest, Detail: "The request body is incomplete."}
	}),
	Status(context.DeadlineExceeded, http.StatusGatewayTimeout),
}
