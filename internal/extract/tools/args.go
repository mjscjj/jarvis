package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// decodeToolArgs strictly decodes a model-produced tool argument payload into T.
// Unknown fields and trailing content are rejected (fail-fast) so a malformed
// tool call is surfaced instead of silently ignoring extra or missing data.
func decodeToolArgs[T any](arguments json.RawMessage) (T, error) {
	var target T
	if len(bytes.TrimSpace(arguments)) == 0 {
		return target, fmt.Errorf("tool arguments are empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil {
		return target, fmt.Errorf("decode tool arguments: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return target, fmt.Errorf("tool arguments contain multiple JSON values")
		}
		return target, fmt.Errorf("decode trailing tool arguments: %w", err)
	}
	return target, nil
}
