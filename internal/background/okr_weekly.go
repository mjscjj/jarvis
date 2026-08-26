package background

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/progress"
)

const maxOKRWeeklyWindow = 8 * 24 * time.Hour

// OKRWeeklyView is a read-only projection over the current Page conclusions
// and the append-only Fact history for one OKR hierarchy. It deliberately does
// not create Tasks or introduce another progress persistence model.
type OKRWeeklyView struct {
	OKR          OKRView         `json:"okr"`
	From         time.Time       `json:"from"`
	Until        time.Time       `json:"until"`
	ChangeCount  int             `json:"change_count"`
	RiskCount    int             `json:"risk_count"`
	StalledCount int             `json:"stalled_count"`
	Items        []OKRWeeklyItem `json:"items"`
}

// OKRWeeklyItem keeps the current conclusion next to the facts that changed in
// the selected week. Parent fields let clients render the durable hierarchy
// without inferring it from titles.
type OKRWeeklyItem struct {
	SubjectType  string              `json:"subject_type"`
	SubjectID    uint64              `json:"subject_id"`
	ParentType   *string             `json:"parent_type"`
	ParentID     *uint64             `json:"parent_id"`
	Title        string              `json:"title"`
	Status       string              `json:"status"`
	Summary      *string             `json:"summary"`
	LastProgress *time.Time          `json:"last_progress_at"`
	Signal       string              `json:"signal"`
	SignalReason string              `json:"signal_reason"`
	Facts        []progress.FactView `json:"facts"`
	ChangeCount  int                 `json:"change_count"`
	CreatedAt    time.Time           `json:"created_at"`
	DueAt        *time.Time          `json:"due_at"`
	ClosedAt     *time.Time          `json:"closed_at"`
}

// WeeklyView derives a deterministic weekly change/risk projection from the
// existing world model. Page remains the current conclusion and Fact remains
// the only history source.
func (s *OKRService) WeeklyView(ctx context.Context, id uint64, from, until time.Time) (*OKRWeeklyView, error) {
	from = from.UTC()
	until = until.UTC()
	if from.IsZero() || until.IsZero() || !from.Before(until) {
		return nil, invalid(fmt.Errorf("from and until must define a non-empty half-open window"))
	}
	if until.Sub(from) > maxOKRWeeklyWindow {
		return nil, invalid(fmt.Errorf("weekly window must not exceed 8 days"))
	}
	okr, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	result := &OKRWeeklyView{OKR: *okr, From: from, Until: until, Items: make([]OKRWeeklyItem, 0, 1+len(okr.Projects)*2)}
	root := OKRWeeklyItem{
		SubjectType: "okr", SubjectID: okr.ID, Title: okr.Title, Status: okr.Status,
		Summary: okr.Summary, LastProgress: okr.LastProgressAt, CreatedAt: okr.CreatedAt, ClosedAt: okr.ClosedAt,
	}
	if err := s.appendWeeklyItem(ctx, result, root); err != nil {
		return nil, err
	}
	for _, project := range okr.Projects {
		parentType, parentID := "okr", okr.ID
		item := OKRWeeklyItem{
			SubjectType: "project", SubjectID: project.ID, ParentType: &parentType, ParentID: &parentID,
			Title: project.Name, Status: project.Status, Summary: project.Summary,
			LastProgress: project.LastProgressAt, CreatedAt: project.CreatedAt,
		}
		if err := s.appendWeeklyItem(ctx, result, item); err != nil {
			return nil, err
		}
		for _, matter := range project.KeyMatters {
			matterParentType, matterParentID := "project", project.ID
			item := OKRWeeklyItem{
				SubjectType: "key_matter", SubjectID: matter.ID, ParentType: &matterParentType, ParentID: &matterParentID,
				Title: matter.Title, Status: matter.Status, Summary: matter.Summary,
				LastProgress: matter.LastProgressAt, CreatedAt: matter.CreatedAt,
				DueAt: matter.DueAt, ClosedAt: matter.ClosedAt,
			}
			if err := s.appendWeeklyItem(ctx, result, item); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func (s *OKRService) appendWeeklyItem(ctx context.Context, result *OKRWeeklyView, item OKRWeeklyItem) error {
	facts, err := s.events.ListFacts(ctx, progress.FactFilter{
		SubjectType:       item.SubjectType,
		SubjectID:         item.SubjectID,
		From:              &result.From,
		Until:             &result.Until,
		Limit:             200,
		ExcludeSourceKind: stringPointer(progress.FactSourceRollup),
	})
	if err != nil {
		return err
	}
	item.Facts = facts
	item.ChangeCount = len(facts)
	item.Signal, item.SignalReason = weeklySignal(item, result.From, result.Until, s.now().UTC())
	result.ChangeCount += item.ChangeCount
	switch item.Signal {
	case "risk":
		result.RiskCount++
	case "stalled":
		result.StalledCount++
	}
	result.Items = append(result.Items, item)
	return nil
}

func weeklySignal(item OKRWeeklyItem, from, until, now time.Time) (string, string) {
	if weeklyItemClosed(item) {
		return "complete", "已完成或已闭环"
	}
	text := item.Status
	for _, fact := range item.Facts {
		text += " " + fact.Description
	}
	if containsWeeklyRisk(text) {
		return "risk", "状态或本周事实包含明确风险信号"
	}
	asOf := until
	if now.Before(asOf) {
		asOf = now
	}
	if item.DueAt != nil && item.DueAt.Before(asOf) {
		return "risk", "关键事项已过截止时间"
	}
	if len(item.Facts) == 0 {
		latest := item.CreatedAt
		if item.LastProgress != nil && item.LastProgress.After(latest) {
			latest = *item.LastProgress
		}
		if latest.Before(from) {
			return "stalled", "本周没有进展事实，最近实质进展早于本周"
		}
	}
	return "steady", "本周有变化或最近实质进展仍在本周"
}

func weeklyItemClosed(item OKRWeeklyItem) bool {
	if item.ClosedAt != nil {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	for _, value := range []string{"done", "archived", "closed", "complete", "completed", "完成", "已完成", "已闭环"} {
		if status == value {
			return true
		}
	}
	return false
}

func containsWeeklyRisk(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{"risk", "blocked", "delay", "overdue", "风险", "延期", "延误", "阻塞", "卡住"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string { return &value }
