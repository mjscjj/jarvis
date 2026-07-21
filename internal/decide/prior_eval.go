package decide

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// maxPriorEvalsInPrompt caps how many previous M4 evaluation events ride into
// the next decision prompt. Oldest beyond the cap are dropped.
const maxPriorEvalsInPrompt = 5

// PriorEvaluation is a compact summary of one earlier M4 evaluation for a Todo.
// It is injected into re-evaluation prompts so Codex knows what it already asked,
// proposed, and looked up — instead of starting from a blank slate after a
// human supplement.
type PriorEvaluation struct {
	At               string          `json:"at"`
	Route            string          `json:"route"`
	RouteReason      string          `json:"route_reason,omitempty"`
	Clarifications   []Clarification `json:"clarifications,omitempty"`
	ProposedPlan     *PlanDraft      `json:"proposed_plan,omitempty"`
	EvidenceGathered []Evidence      `json:"evidence_gathered,omitempty"`
}

// loadPriorEvaluations reads evaluated todo_event rows for a Todo and returns
// up to maxPriorEvalsInPrompt summaries, oldest-first. A nil db yields an empty
// list (unit tests without a DB). Fail-fast on decode errors.
func loadPriorEvaluations(ctx context.Context, db *gorm.DB, todoID uint64) ([]PriorEvaluation, error) {
	if db == nil || todoID == 0 {
		return nil, nil
	}
	var rows []domain.TodoEvent
	if err := db.WithContext(ctx).
		Where("todo_id = ?", todoID).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load prior evaluations todo_id=%d: %w", todoID, err)
	}
	out := make([]PriorEvaluation, 0)
	for _, row := range rows {
		if len(row.Detail) == 0 {
			continue
		}
		var detail struct {
			EventType        string          `json:"event_type"`
			RouteReason      string          `json:"route_reason"`
			ProposedPlan     *PlanDraft      `json:"proposed_plan"`
			Clarifications   []Clarification `json:"clarifications"`
			EvidenceGathered []Evidence      `json:"evidence_gathered"`
		}
		if err := json.Unmarshal(row.Detail, &detail); err != nil {
			return nil, fmt.Errorf("decode prior evaluation event_id=%d: %w", row.ID, err)
		}
		if detail.EventType != "evaluated" {
			continue
		}
		out = append(out, PriorEvaluation{
			At:               row.CreatedAt.UTC().Format(time.RFC3339),
			Route:            row.ToStatus,
			RouteReason:      detail.RouteReason,
			Clarifications:   detail.Clarifications,
			ProposedPlan:     detail.ProposedPlan,
			EvidenceGathered: detail.EvidenceGathered,
		})
	}
	if len(out) > maxPriorEvalsInPrompt {
		out = out[len(out)-maxPriorEvalsInPrompt:]
	}
	return out, nil
}
