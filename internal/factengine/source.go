// Package factengine is the offline fact engine: a cron-driven agent that reads
// material the pipeline already produced and distils long-lived facts out of it.
//
// It runs off the M2→M3→M5 critical path. Nothing upstream waits for it, and a
// failed round costs at most one retry — the source watermark only advances past
// material whose facts are already committed.
//
// One extraction protocol serves every source. A source contributes a SQL
// projection that renders material into SourceUnit; what counts as a fact and
// how to bind it to a subject live in the prompt, not here. Adding the Todo and
// Task sources is a new projection, not a new pipeline.
package factengine

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SourceUnit is one piece of material to distil facts from: a conversation
// window, a Todo, a finished Task run. Body is already-rendered natural
// language, because the model reads it and Go never parses it back.
type SourceUnit struct {
	// Source names the material's origin ("message" today). It is carried into
	// the prompt so the model knows what it is reading, and into fact.source_kind
	// so a stored fact traces back to what produced it.
	Source string

	// Key identifies this unit for logging and error messages. It is not a
	// dedup key: duplicate material is the consolidation step's problem.
	Key string

	// LastID is the highest database id this unit consumed. The worker advances
	// the source cursor to it once the unit's facts are committed.
	LastID uint64

	// OccurredAt is when the material happened; it becomes the fact's
	// occurred_at, so a fact lands on the day of the conversation rather than
	// the day the engine got around to reading it.
	OccurredAt time.Time

	Body string

	// Subjects are the entities this material may be about, with the real
	// database ids the model must choose from. Referential integrity is the
	// program's job; which subject a fact belongs to is the model's.
	Subjects []Subject
}

// Subject is one candidate owner of a fact. Type mirrors fact.subject_type and
// stays a free string: the engine offers the subjects it can resolve, and the
// prompt decides what to do with them.
type Subject struct {
	Type string
	ID   uint64
	Name string
}

// Prompt renders the user half of one extraction call. The system half is the
// fact_extract_system_prompt text file.
func (u SourceUnit) Prompt() (string, error) {
	if strings.TrimSpace(u.Body) == "" {
		return "", fmt.Errorf("source unit %s/%s has an empty body", u.Source, u.Key)
	}
	if len(u.Subjects) == 0 {
		return "", fmt.Errorf("source unit %s/%s has no available subjects", u.Source, u.Key)
	}
	subjects, err := json.MarshalIndent(u.Subjects, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode subjects for source unit %s/%s: %w", u.Source, u.Key, err)
	}
	var b strings.Builder
	b.WriteString("MATERIAL_SOURCE: ")
	b.WriteString(u.Source)
	b.WriteString("\n\nAVAILABLE_SUBJECTS（subject_id 只能从这里取）:\n")
	b.Write(subjects)
	b.WriteString("\n\nMATERIAL:\n")
	b.WriteString(u.Body)
	return b.String(), nil
}

// MarshalJSON keeps the subject list readable in the prompt: the model sees the
// same three words the fact table stores.
func (s Subject) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"subject_type": s.Type,
		"subject_id":   s.ID,
		"name":         s.Name,
	})
}
