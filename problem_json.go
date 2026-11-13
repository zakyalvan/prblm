package prblm

import (
	"encoding/json"
	"strconv"
)

// problemJSON is the wire form of the standard members. Its field order is
// the member order of the encoded document, so the output is deterministic.
type problemJSON struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   int    `json:"status,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// decodeStatus returns v as a status code, or 0 when v is not an integer.
func decodeStatus(v any) int {
	n, ok := v.(json.Number)
	if !ok {
		return 0
	}

	status, err := strconv.Atoi(n.String())
	if err != nil {
		return 0
	}

	return status
}
