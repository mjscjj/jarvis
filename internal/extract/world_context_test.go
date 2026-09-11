package extract

import (
	"context"
	"strings"
	"testing"
	"time"

	"jarvis/internal/progress"
)

func TestLoadFactCountsTodayAndLast7Days(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, loc)
	todayStart := time.Date(2026, 8, 2, 0, 0, 0, 0, loc)
	tomorrow := todayStart.AddDate(0, 0, 1)
	weekStart := todayStart.AddDate(0, 0, -6)

	reader := &scriptedFactCounter{byQuery: func(filter progress.FactFilter) int {
		if filter.From == nil || filter.Until == nil {
			t.Fatalf("count window missing: %#v", filter)
		}
		switch {
		case filter.From.Equal(todayStart) && filter.Until.Equal(tomorrow):
			if filter.SubjectType == "group" {
				return 2
			}
			return 23
		case filter.From.Equal(weekStart) && filter.Until.Equal(tomorrow):
			if filter.SubjectType == "group" {
				return 10
			}
			return 187
		default:
			t.Fatalf("unexpected filter: %#v", filter)
			return 0
		}
	}}

	worker := &Worker{
		facts: reader,
		opts:  WorkerOptions{Location: loc},
	}
	projectID := uint64(44)
	counts, err := worker.loadFactCounts(context.Background(), ChatBatch{
		Group:   GroupContext{ID: 4, ChatID: "oc_1", Name: "公会群", ProjectID: &projectID},
		Project: &ProjectContext{ID: 44, Name: "公会 Agent 基建"},
	}, now)
	if err != nil {
		t.Fatalf("loadFactCounts: %v", err)
	}
	if len(reader.calls) != 4 {
		t.Fatalf("CountFacts calls = %d, want 4 (group+project × today+week)", len(reader.calls))
	}
	if len(counts) != 2 {
		t.Fatalf("counts = %#v, want group+project", counts)
	}
	if counts[0].SubjectType != "group" || counts[0].Today != 2 || counts[0].Last7Days != 10 || counts[0].Label != "公会群" {
		t.Fatalf("group count = %#v", counts[0])
	}
	if counts[1].SubjectType != "project" || counts[1].SubjectID != 44 || counts[1].Today != 23 || counts[1].Last7Days != 187 || counts[1].Label != "公会 Agent 基建" {
		t.Fatalf("project count = %#v", counts[1])
	}
}

func TestBuildPromptDropsContextOnlyWhenOverBudget(t *testing.T) {
	t.Parallel()
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{
		{MessageID: "om_ctx", Content: "CONTEXT_MARKER unique-context-line", CreateTime: 1_700_000_000_000, IsNew: false, Extractable: true},
		{MessageID: "om_new", Content: "NEW_MARKER unique-new-line", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true},
	}}
	otherProjects := make([]OtherProjectContext, 0, 6)
	for i := 0; i < 6; i++ {
		otherProjects = append(otherProjects, OtherProjectContext{
			ID: uint64(i + 1), Code: "p", Name: "proj-" + string(rune('a'+i)), Role: "owner",
		})
	}
	batch := ChatBatch{
		Group:         GroupContext{ChatID: "oc_1"},
		OtherProjects: otherProjects,
	}
	counts := []FactCount{{
		SubjectType: "group", SubjectID: 1, Label: "研发群", Today: 3, Last7Days: 12,
	}}

	full, err := BuildPrompt(batch, unit, counts, time.Unix(1_700_000_100, 0), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 200_000,
	})
	if err != nil {
		t.Fatalf("full BuildPrompt: %v", err)
	}
	if !strings.Contains(full.User, "CONTEXT_MARKER") || !strings.Contains(full.User, "NEW_MARKER") {
		t.Fatalf("full prompt missing conversation markers")
	}
	if !strings.Contains(full.User, "proj-a") || !strings.Contains(full.User, "今日 3 条") {
		t.Fatalf("full prompt missing world sections:\n%s", full.User)
	}

	tight := len([]rune(full.System)) + len([]rune(full.User)) - 80
	if tight < 2000 {
		t.Fatalf("unexpected full prompt size %d", tight)
	}
	shrunk, err := BuildPrompt(batch, unit, counts, time.Unix(1_700_000_100, 0), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: tight,
	})
	if err != nil {
		t.Fatalf("tight BuildPrompt: %v", err)
	}
	if strings.Contains(shrunk.User, "CONTEXT_MARKER") {
		t.Fatalf("tight prompt kept context message:\n%s", shrunk.User)
	}
	if !strings.Contains(shrunk.User, "NEW_MARKER") {
		t.Fatalf("tight prompt lost new message:\n%s", shrunk.User)
	}
	if !strings.Contains(shrunk.User, "proj-a") || !strings.Contains(shrunk.User, "今日 3 条") {
		t.Fatalf("tight prompt trimmed world data:\n%s", shrunk.User)
	}
}

type scriptedFactCounter struct {
	calls   []progress.FactFilter
	byQuery func(progress.FactFilter) int
}

func (s *scriptedFactCounter) CountFacts(_ context.Context, filter progress.FactFilter) (int, error) {
	s.calls = append(s.calls, filter)
	if s.byQuery == nil {
		return 0, nil
	}
	return s.byQuery(filter), nil
}
