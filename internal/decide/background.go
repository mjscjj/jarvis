package decide

import "encoding/json"

// rawJSON copies a byte slice into a json.RawMessage, returning JSON null for an
// empty input. It is the shared helper for projecting stored JSON columns into
// API views (see prompt.go / detail.go).
//
// M4 no longer builds a background snapshot here: the background is the
// context_snapshot M3 freezes onto the Todo, which M4 reuses verbatim (see
// service.go requireContextSnapshot and docs/design-context-pipeline.md §2.2).
func rawJSON(value []byte) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(append([]byte(nil), value...))
}
