package okrworkspace

import (
	"context"
	"fmt"
	"strings"
)

// ReplaceKRDefinitionInput carries only fields owned by the KR row. Concrete
// points have their own endpoint and version; a stale KR snapshot must never be
// able to rewrite a point edited by somebody else.
type ReplaceKRDefinitionInput struct {
	ExpectedVersion int32       `json:"expected_version"`
	Title           string      `json:"title"`
	Owners          []OwnerView `json:"owners"`
	UpdatedBy       string      `json:"-"`
}

func (s *Service) ReplaceKRDefinition(ctx context.Context, id string, input ReplaceKRDefinitionInput) (KRView, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return KRView{}, fmt.Errorf("kr_id is required")
	}
	if input.ExpectedVersion < 0 {
		return KRView{}, fmt.Errorf("expected_version must be non-negative")
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return KRView{}, fmt.Errorf("kr title is required")
	}
	db := s.db.WithContext(ctx)
	if err := updateKRVersion(db, id, input.ExpectedVersion, map[string]any{
		"title": input.Title, "updated_by": input.UpdatedBy,
	}); err != nil {
		return KRView{}, err
	}
	if err := replaceKROwners(db, id, normalizeOwners(input.Owners)); err != nil {
		return KRView{}, err
	}
	return s.GetCoreKR(ctx, id)
}
