// Package factengine is the offline world-model engine: a cron-driven agent that
// reads material the pipeline already produced, distils long-lived facts and
// uses generic Jarvis tools to maintain current internal entities and relations.
//
// It runs off the M2→M3→M5 critical path. Nothing upstream waits for it, and a
// failed round costs at most one retry — source watermarks advance only after the
// complete maintenance session succeeds.
//
// One Agent protocol serves every source. A source contributes a SQL projection
// that renders material into SourceUnit; what counts as a fact and whether any
// world object should change live in the prompt, not here. Adding the Todo and
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
	// Source names the material's origin (message, todo, task today). It is carried into
	// the prompt so the model knows what it is reading, and into fact.source_kind
	// so a stored fact traces back to what produced it.
	Source string

	// Key identifies this unit for logging and error messages. It is not a
	// dedup key: duplicate material is the consolidation step's problem.
	Key string

	// LastID is the highest database id this unit consumed. The worker advances
	// each source cursor after the combined maintenance session succeeds.
	LastID uint64

	// OccurredAt is when the material happened; it becomes the fact's
	// occurred_at, so a fact lands on the day of the conversation rather than
	// the day the engine got around to reading it.
	OccurredAt time.Time

	// Context is a loose, source-owned description of the world around the
	// material. The fact engine passes it through verbatim: a new source may add
	// whatever background helps the agent understand the material without
	// expanding a shared DTO or teaching the worker source-specific fields.
	Context string

	Body string

	// Subjects are known entities surfaced by the source as useful context. They
	// are hints, not an allowlist: the agent may resolve a better subject from the
	// context or with tools. The persistence layer owns the hard integrity check
	// for subject types it knows.
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

// Prompt renders one complete material block. The worker combines all selected
// blocks into the user half of one world-maintenance Agent session.
func (u SourceUnit) Prompt() (string, error) {
	if strings.TrimSpace(u.Body) == "" {
		return "", fmt.Errorf("source unit %s/%s has an empty body", u.Source, u.Key)
	}
	var b strings.Builder
	b.WriteString("MATERIAL_SOURCE: ")
	b.WriteString(u.Source)
	b.WriteString("\nMATERIAL_KEY: ")
	b.WriteString(u.Key)
	if !u.OccurredAt.IsZero() {
		b.WriteString("\nMATERIAL_OCCURRED_AT: ")
		b.WriteString(u.OccurredAt.Format(time.RFC3339))
	}
	if strings.TrimSpace(u.Context) != "" {
		b.WriteString("\n\nCONTEXT（宽松背景，只帮助理解，不是输出模板）:\n")
		b.WriteString(strings.TrimSpace(u.Context))
	}
	if len(u.Subjects) > 0 {
		subjects, err := json.MarshalIndent(u.Subjects, "", "  ")
		if err != nil {
			return "", fmt.Errorf("encode subjects for source unit %s/%s: %w", u.Source, u.Key, err)
		}
		b.WriteString("\n\nKNOWN_ENTITIES（已知实体提示，不是白名单）:\n")
		b.Write(subjects)
	}
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
