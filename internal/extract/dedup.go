package extract

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/semantic"
)

var activeTodoStatuses = map[string]struct{}{
	"extracted": {}, "scoring": {}, "need_info": {}, "need_decision": {}, "confirmed": {},
}

func ActiveTodoStatuses() []string {
	return []string{"extracted", "scoring", "need_info", "need_decision", "confirmed"}
}

type SemanticTodo struct {
	ID               uint64  `json:"id"`
	ActionType       string  `json:"action_type"`
	Title            string  `json:"title"`
	Description      string  `json:"description"`
	Target           string  `json:"target"`
	ProjectID        *uint64 `json:"project_id"`
	Status           string  `json:"status"`
	DedupFingerprint string  `json:"dedup_fingerprint"`
}

type semanticEmbedder interface {
	Embed(context.Context, string) ([]float32, error)
}

type semanticSearcher interface {
	Search(context.Context, []float32, *uint64, string) ([]semantic.Match, error)
}

type semanticTodoLoader interface {
	LoadSemanticTodo(context.Context, uint64) (*SemanticTodo, error)
}

type semanticAdjudicator interface {
	SameAction(context.Context, Candidate, SemanticTodo) (bool, error)
}

type Deduplicator struct {
	embedder    semanticEmbedder
	index       semanticSearcher
	store       semanticTodoLoader
	adjudicator semanticAdjudicator
}

func NewDeduplicator(embedder semanticEmbedder, index semanticSearcher, store semanticTodoLoader, adjudicator semanticAdjudicator) (*Deduplicator, error) {
	if embedder == nil || index == nil || store == nil || adjudicator == nil {
		return nil, fmt.Errorf("semantic deduplicator dependencies must not be nil")
	}
	return &Deduplicator{embedder: embedder, index: index, store: store, adjudicator: adjudicator}, nil
}

func (d *Deduplicator) Resolve(ctx context.Context, candidate Candidate, projectID *uint64) (SemanticResolution, error) {
	fingerprint, err := Fingerprint(&candidate, projectID)
	if err != nil {
		return SemanticResolution{}, err
	}
	text, err := SemanticText(&candidate)
	if err != nil {
		return SemanticResolution{}, err
	}
	vector, err := d.embedder.Embed(ctx, text)
	if err != nil {
		return SemanticResolution{}, fmt.Errorf("embed Todo candidate: %w", err)
	}
	matches, err := d.index.Search(ctx, vector, projectID, candidate.ActionType)
	if err != nil {
		return SemanticResolution{}, err
	}
	resolution := SemanticResolution{Vector: vector}
	var confirmed *uint64
	for _, match := range matches {
		existing, err := d.store.LoadSemanticTodo(ctx, match.TodoID)
		if err != nil {
			return SemanticResolution{}, fmt.Errorf("load semantic neighbor todo_id=%d: %w", match.TodoID, err)
		}
		if existing == nil {
			return SemanticResolution{}, fmt.Errorf("load semantic neighbor todo_id=%d: nil Todo", match.TodoID)
		}
		if existing.DedupFingerprint != match.Fingerprint {
			return SemanticResolution{}, fmt.Errorf("semantic index fingerprint mismatch todo_id=%d", match.TodoID)
		}
		if existing.ActionType != candidate.ActionType || !sameUint64(existing.ProjectID, projectID) {
			return SemanticResolution{}, fmt.Errorf("semantic index domain mismatch todo_id=%d", match.TodoID)
		}
		if _, active := activeTodoStatuses[existing.Status]; !active {
			return SemanticResolution{}, fmt.Errorf("semantic index contains inactive todo_id=%d status=%s", match.TodoID, existing.Status)
		}
		if match.Fingerprint == fingerprint {
			id := existing.ID
			resolution.MatchedTodoID = &id
			return resolution, nil
		}
		same, err := d.adjudicator.SameAction(ctx, candidate, *existing)
		if err != nil {
			return SemanticResolution{}, fmt.Errorf("adjudicate semantic neighbor todo_id=%d: %w", match.TodoID, err)
		}
		if !same {
			continue
		}
		if confirmed != nil {
			return SemanticResolution{}, fmt.Errorf("semantic candidate matches multiple Todos: %d and %d", *confirmed, existing.ID)
		}
		id := existing.ID
		confirmed = &id
	}
	resolution.MatchedTodoID = confirmed
	return resolution, nil
}

func (s *PipelineStore) LoadSemanticTodo(ctx context.Context, todoID uint64) (*SemanticTodo, error) {
	if todoID == 0 {
		return nil, fmt.Errorf("semantic Todo ID must be positive")
	}
	var row struct {
		ID               uint64
		ActionType       string
		Title            string
		Description      string
		Target           string
		ProjectID        *uint64
		Status           string
		DedupFingerprint string
	}
	result := s.db.WithContext(ctx).Table("todo").Where("id = ?", todoID).Take(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if strings.TrimSpace(row.DedupFingerprint) == "" {
		return nil, fmt.Errorf("semantic Todo fingerprint is empty todo_id=%d", todoID)
	}
	return &SemanticTodo{
		ID: row.ID, ActionType: row.ActionType, Title: row.Title, Description: row.Description,
		Target: row.Target, ProjectID: copyUint64(row.ProjectID), Status: row.Status,
		DedupFingerprint: row.DedupFingerprint,
	}, nil
}

func sameUint64(left, right *uint64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
