package execute

// The decision stage builds no background snapshot of its own: the background is
// the context_snapshot M3 freezes onto the Todo, reused verbatim (see
// requireContextSnapshot in review.go and docs/design-context-pipeline.md §2.2).
// JSON projection uses the shared rawJSON helper in store.go.

// copyString returns a defensive copy of a string pointer, used when projecting
// stored rows into API views so callers cannot mutate the source.
func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
