package okrworkspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"jarvis/internal/okrworkspace/domain"
)

type fakeRegionalTranslator struct {
	cacheKey string
	calls    [][]string
}

func (f *fakeRegionalTranslator) CacheKey() string {
	if f.cacheKey == "" {
		return "test-glossary-v1"
	}
	return f.cacheKey
}

func (f *fakeRegionalTranslator) Translate(_ context.Context, texts []string) (map[string]string, error) {
	f.calls = append(f.calls, append([]string(nil), texts...))
	result := make(map[string]string, len(texts))
	for _, source := range texts {
		result[source] = "EN: " + source
	}
	return result, nil
}

func TestRefreshRegionalAlignmentUsesLatestReviewProgressAndCachesEnglish(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q4", Title: "Q4 Plan", CreatedBy: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = service.CreatePlanObjective(t.Context(), plan.ID, PlanObjectiveView{
		ID: "plan-o", Title: "计划目标", KRs: []PlanKRView{{
			ID: "plan-kr", Title: "计划关键结果", MetricNote: "计划指标说明",
			Metrics: []MetricView{{ID: "plan-metric", Text: "指标达到 100%"}},
			Points:  []PlanPointView{{ID: "plan-point", Kind: domain.PointKindStrategy, Title: "计划具体 KR"}},
		}},
	}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []any{
		&domain.Objective{ID: "review-o", Quarter: "2026-Q3", Title: "复盘目标", Version: 1, CreatedAt: now, UpdatedAt: now},
		&domain.KR{ID: "review-kr", ObjectiveID: "review-o", Title: "复盘关键结果", MetricNote: "复盘指标说明", Version: 1, CreatedAt: now, UpdatedAt: now},
		&domain.KRMetric{ID: "review-metric", KRID: "review-kr", Text: "复盘指标 80%"},
		&domain.KRPoint{ID: "review-point", KRID: "review-kr", Version: 1, Kind: domain.PointKindProduct, Title: "复盘具体 KR"},
		&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "owner", OpenedAt: now.Add(-time.Hour)},
		&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W36", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "owner", OpenedAt: now},
		&domain.KRProgress{ID: "review-progress", PointID: "review-point", Week: "2026-W36", Version: 1, Status: domain.StatusInProgress, Text: "最新复盘进展", Source: "manual", CreatedAt: now, UpdatedAt: now},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	translator := &fakeRegionalTranslator{}
	if err := service.SetRegionalTranslator(translator); err != nil {
		t.Fatal(err)
	}

	board, err := service.RefreshRegionalAlignmentBoard(t.Context(), "2026-Q4", "eu", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if board.Alignment.PlanID != plan.ID || board.Recap.Week != "2026-W36" {
		t.Fatalf("alignment source selection = plan %q, recap week %q", board.Alignment.PlanID, board.Recap.Week)
	}
	if len(board.Recap.Objectives) != 1 || len(board.Recap.Objectives[0].KRs[0].Points[0].Entries) != 1 || board.Recap.Objectives[0].KRs[0].Points[0].Entries[0].Text != "最新复盘进展" {
		t.Fatalf("latest review progress missing from recap: %+v", board.Recap)
	}
	for _, source := range []string{"计划目标", "计划关键结果", "计划指标说明", "指标达到 100%", "计划具体 KR", "复盘目标", "复盘关键结果", "复盘指标说明", "复盘指标 80%", "复盘具体 KR", "最新复盘进展"} {
		if got := board.Translations[source]; got != "EN: "+source {
			t.Fatalf("translation[%q] = %q", source, got)
		}
	}
	if len(translator.calls) != 1 {
		t.Fatalf("translator calls = %d, want 1", len(translator.calls))
	}

	translator.calls = nil
	cached, err := service.RefreshRegionalAlignmentBoard(t.Context(), "2026-Q4", "eu", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(translator.calls) != 0 || !strings.HasPrefix(cached.Translations["最新复盘进展"], "EN: ") {
		t.Fatalf("cached refresh called translator or lost English: calls=%d translations=%+v", len(translator.calls), cached.Translations)
	}

	replacement := &fakeRegionalTranslator{cacheKey: "test-glossary-v2"}
	if err := service.SetRegionalTranslator(replacement); err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.RefreshRegionalAlignmentBoard(t.Context(), "2026-Q4", "eu", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if len(replacement.calls) != 1 || !strings.HasPrefix(refreshed.Translations["最新复盘进展"], "EN: ") {
		t.Fatalf("glossary change did not invalidate cached translations: calls=%d translations=%+v", len(replacement.calls), refreshed.Translations)
	}
	var translated domain.OKRTranslation
	if err := db.Where("source_hash = ?", regionalTranslationHash("最新复盘进展")).First(&translated).Error; err != nil {
		t.Fatal(err)
	}
	if translated.GlossaryHash != "test-glossary-v2" {
		t.Fatalf("stored glossary hash = %q", translated.GlossaryHash)
	}
}

func TestRegionalViewsEncodeMissingOwnersAsEmptyArrays(t *testing.T) {
	for _, row := range []domain.RegionalDemand{
		{},
		{RegionalPOCs: []domain.FollowUpOwner{}, PlatformPOCs: []domain.FollowUpOwner{}},
	} {
		demand := regionalDemandView(row)
		if demand.RegionalPOCs == nil || demand.PlatformPOCs == nil {
			t.Fatalf("demand owner arrays must be non-nil: regional=%v platform=%v", demand.RegionalPOCs, demand.PlatformPOCs)
		}
	}
	for _, row := range []domain.RegionalPlanDecision{
		{},
		{RegionalPOCs: []domain.FollowUpOwner{}},
	} {
		decision := regionalDecisionView(row)
		if decision.RegionalPOCs == nil {
			t.Fatal("decision owner array must be non-nil")
		}
	}
}
