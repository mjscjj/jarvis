package okrworkspace

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// Reordering carries the whole sibling list rather than one move, so ties and
// gaps in the stored sort order cannot make a move silently do nothing, and a
// drag gesture would need no new contract. It deliberately leaves KR.version
// alone: position is not content, and bumping it would turn every page open
// elsewhere into a stale baseline.
func (s *Service) ReorderObjectives(ctx context.Context, quarter string, ids, expectedOrder []string) ([]string, error) {
	quarter = strings.TrimSpace(quarter)
	if quarter == "" {
		return nil, fmt.Errorf("quarter is required")
	}
	db := s.db.WithContext(ctx)
	var records []domain.Objective
	if err := db.Where("quarter = ? AND plan_id = ''", quarter).Order("sort_order, id").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list objectives for reorder: %w", err)
	}
	known := make(map[string]struct{}, len(records))
	for _, record := range records {
		known[record.ID] = struct{}{}
	}
	if !sameOrder(expectedOrder, objectiveIDs(records)) {
		return nil, ErrConflict
	}
	ordered, err := completeOrder(ids, known, fmt.Sprintf("quarter %s", quarter))
	if err != nil {
		return nil, err
	}
	for index, id := range ordered {
		if err := db.Model(&domain.Objective{}).Where("id = ? AND plan_id = ''", id).Update("sort_order", index).Error; err != nil {
			return nil, fmt.Errorf("write objective sort order: %w", err)
		}
	}
	return ordered, nil
}

func (s *Service) ReorderKRs(ctx context.Context, objectiveID string, ids, expectedOrder []string) ([]string, error) {
	objectiveID = strings.TrimSpace(objectiveID)
	if objectiveID == "" {
		return nil, fmt.Errorf("objective_id is required")
	}
	db := s.db.WithContext(ctx)
	var objective domain.Objective
	if err := db.First(&objective, "id = ?", objectiveID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get objective for reorder: %w", err)
	}
	if objective.PlanID != "" {
		return nil, ErrNotFound
	}
	var records []domain.KR
	if err := db.Where("objective_id = ?", objectiveID).Order("sort_order, id").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list krs for reorder: %w", err)
	}
	known := make(map[string]struct{}, len(records))
	for _, record := range records {
		known[record.ID] = struct{}{}
	}
	current := make([]string, 0, len(records))
	for _, record := range records {
		current = append(current, record.ID)
	}
	if !sameOrder(expectedOrder, current) {
		return nil, ErrConflict
	}
	ordered, err := completeOrder(ids, known, fmt.Sprintf("objective %s", objectiveID))
	if err != nil {
		return nil, err
	}
	for index, id := range ordered {
		if err := db.Model(&domain.KR{}).Where("id = ? AND objective_id = ?", id, objectiveID).Update("sort_order", index).Error; err != nil {
			return nil, fmt.Errorf("write kr sort order: %w", err)
		}
	}
	return ordered, nil
}

func objectiveIDs(records []domain.Objective) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

func sameOrder(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if strings.TrimSpace(left[index]) != right[index] {
			return false
		}
	}
	return true
}

// A partial list would leave the rows it omits at an arbitrary position, so the
// request has to name every sibling exactly once.
func completeOrder(ids []string, known map[string]struct{}, scope string) ([]string, error) {
	if len(known) == 0 {
		return nil, ErrNotFound
	}
	ordered := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("order contains an empty id")
		}
		if _, ok := known[id]; !ok {
			return nil, fmt.Errorf("%s does not contain %s", scope, id)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("%s appears twice in the order", id)
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	if len(ordered) != len(known) {
		return nil, fmt.Errorf("order must list all %d rows of %s, got %d", len(known), scope, len(ordered))
	}
	return ordered, nil
}
