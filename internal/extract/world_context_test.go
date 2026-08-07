package extract

import (
	"context"
	"strings"
	"testing"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/progress"
)

type scriptedFactReader struct {
	calls   []progress.FactFilter
	byQuery func(progress.FactFilter) []progress.FactView
}

func (s *scriptedFactReader) ListFacts(_ context.Context, filter progress.FactFilter) ([]progress.FactView, error) {
	s.calls = append(s.calls, filter)
	if s.byQuery == nil {
		return nil, nil
	}
	return s.byQuery(filter), nil
}

func TestLoadFactsTwoLayers(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, loc)
	todayStart := time.Date(2026, 8, 2, 0, 0, 0, 0, loc)
	tomorrow := todayStart.AddDate(0, 0, 1)
	yesterday := todayStart.AddDate(0, 0, -1)
	rollup := progress.FactSourceRollup

	reader := &scriptedFactReader{byQuery: func(filter progress.FactFilter) []progress.FactView {
		switch {
		case filter.ExcludeSourceKind != nil && *filter.ExcludeSourceKind == progress.FactSourceRollup:
			if filter.From == nil || filter.Until == nil || !filter.From.Equal(todayStart) || !filter.Until.Equal(tomorrow) {
				t.Fatalf("today detail window = %v/%v, want %v/%v", filter.From, filter.Until, todayStart, tomorrow)
			}
			if filter.Limit != 10 {
				t.Fatalf("today detail limit = %d, want 10", filter.Limit)
			}
			return []progress.FactView{{
				ID: 1, SubjectType: filter.SubjectType, SubjectID: filter.SubjectID,
				Description: "今天明细", OccurredAt: todayStart.Add(2 * time.Hour),
			}}
		case filter.SourceKind != nil && *filter.SourceKind == progress.FactSourceRollup:
			if filter.From == nil || filter.Until == nil || !filter.From.Equal(yesterday) || !filter.Until.Equal(todayStart) {
				t.Fatalf("yesterday rollup window = %v/%v, want %v/%v", filter.From, filter.Until, yesterday, todayStart)
			}
			if filter.Limit != 1 {
				t.Fatalf("rollup limit = %d, want 1", filter.Limit)
			}
			return []progress.FactView{{
				ID: 2, SubjectType: filter.SubjectType, SubjectID: filter.SubjectID,
				Description: "昨天压缩", OccurredAt: yesterday, SourceKind: &rollup,
			}}
		default:
			t.Fatalf("unexpected filter: %#v", filter)
			return nil
		}
	}}

	worker := &Worker{
		facts: reader,
		opts:  WorkerOptions{FactLimit: 10, KeyPersonLimit: 5, Location: loc},
	}
	projectID := uint64(9)
	facts, err := worker.loadFacts(context.Background(), ChatBatch{
		Group: GroupContext{ID: 4, ChatID: "oc_1", ProjectID: &projectID},
	}, now)
	if err != nil {
		t.Fatalf("loadFacts: %v", err)
	}
	if len(reader.calls) != 4 {
		t.Fatalf("ListFacts calls = %d, want 4 (group+project × today+rollup)", len(reader.calls))
	}
	if len(facts) != 4 {
		t.Fatalf("facts = %d, want 4", len(facts))
	}
	var sawDetail, sawRollup bool
	for _, fact := range facts {
		switch fact.Description {
		case "今天明细":
			sawDetail = true
		case "昨天压缩":
			sawRollup = true
		}
	}
	if !sawDetail || !sawRollup {
		t.Fatalf("missing layer in facts: %#v", facts)
	}
}

func TestSelectKeyPersonIDsUnionAndCap(t *testing.T) {
	t.Parallel()
	assigner := uint64(11)
	leader := uint64(12)
	speaker := uint64(13)
	extra := uint64(14)
	batch := ChatBatch{
		OpenTodos: []OpenTodoContext{{ID: 1, AssignerPersonID: &assigner}},
		Units: []ConversationUnit{{
			Participants: []ParticipantContext{
				{OpenID: "ou_leader", IsLeader: true, PersonID: &leader},
				{OpenID: "ou_speaker", PersonID: &speaker},
				{OpenID: "ou_extra", PersonID: &extra},
				{OpenID: "ou_unknown", IsLeader: true}, // no PersonID → skip
			},
			Messages: []MessageContext{
				{SenderOpenID: "ou_speaker"},
				{SenderOpenID: "ou_extra"},
			},
		}},
	}
	got := selectKeyPersonIDs(batch, 3)
	if len(got) != 3 {
		t.Fatalf("key persons = %v, want length 3", got)
	}
	if got[0] != assigner || got[1] != leader || got[2] != speaker {
		t.Fatalf("key persons = %v, want assigner→leader→speaker before cap drops extra", got)
	}
	uncapped := selectKeyPersonIDs(batch, 10)
	if len(uncapped) != 4 {
		t.Fatalf("uncapped = %v, want 4 distinct person ids", uncapped)
	}
}

func TestBuildPromptShrinksWorldBeforeConversation(t *testing.T) {
	t.Parallel()
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{
		{MessageID: "om_ctx", Content: "CONTEXT_MARKER unique-context-line", CreateTime: 1_700_000_000_000, IsNew: false, Extractable: true},
		{MessageID: "om_new", Content: "NEW_MARKER unique-new-line", CreateTime: 1_700_000_001_000, IsNew: true, Extractable: true},
	}}
	personFacts := make([]contextsnap.Fact, 0, 6)
	for i := 0; i < 6; i++ {
		personFacts = append(personFacts, contextsnap.Fact{
			ID: uint64(100 + i), SubjectType: "person", SubjectID: 1,
			Description: "person-fact-" + strings.Repeat("x", 40) + "-" + string(rune('a'+i)),
			OccurredAt:  "2026-08-02T01:00:00Z",
		})
	}
	groupFacts := make([]contextsnap.Fact, 0, 6)
	for i := 0; i < 6; i++ {
		groupFacts = append(groupFacts, contextsnap.Fact{
			ID: uint64(200 + i), SubjectType: "group", SubjectID: 1,
			Description: "group-fact-" + strings.Repeat("y", 40) + "-" + string(rune('a'+i)),
			OccurredAt:  "2026-08-02T02:00:00Z",
		})
	}
	otherProjects := make([]OtherProjectContext, 0, 6)
	for i := 0; i < 6; i++ {
		otherProjects = append(otherProjects, OtherProjectContext{
			ID: uint64(i + 1), Code: "p", Name: "proj-" + string(rune('a'+i)), Role: "owner",
			Description: strings.Repeat("other-project-", 8),
		})
	}
	recentTasks := make([]RecentTaskContext, 0, 6)
	for i := 0; i < 6; i++ {
		recentTasks = append(recentTasks, RecentTaskContext{
			ID: uint64(i + 1), Title: "task-" + string(rune('a'+i)), Status: "done",
			Summary: strings.Repeat("task-summary-", 8),
		})
	}
	openTodos := make([]OpenTodoContext, 0, 6)
	for i := 0; i < 6; i++ {
		openTodos = append(openTodos, OpenTodoContext{
			ID: uint64(i + 1), ActionType: "investigate", Title: "todo-" + string(rune('a'+i)), Status: "extracted",
		})
	}
	batch := ChatBatch{
		Group:         GroupContext{ChatID: "oc_1"},
		OtherProjects: otherProjects,
		RecentTasks:   recentTasks,
		OpenTodos:     openTodos,
	}
	facts := append(append([]contextsnap.Fact{}, groupFacts...), personFacts...)

	// Measure a prompt that already fits, then pick a MaxChars that forces world
	// shrinkage but still leaves room for the context message.
	full, err := BuildPrompt(batch, unit, facts, time.Unix(1_700_000_100, 0), PromptOptions{SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 200_000,
	})
	if err != nil {
		t.Fatalf("full BuildPrompt: %v", err)
	}
	if !strings.Contains(full.User, "CONTEXT_MARKER") {
		t.Fatalf("full prompt missing context marker")
	}
	if !strings.Contains(full.User, "person-fact-") || !strings.Contains(full.User, "# 最近有进展的任务") {
		t.Fatalf("full prompt missing world sections:\n%s", full.User)
	}

	// Tight budget: world must shrink first. Keep enough for system + new message
	// + floors, but less than the full world payload.
	tight := len([]rune(full.System)) + len([]rune(full.User)) - 800
	if tight < 2000 {
		t.Fatalf("unexpected full prompt size %d", tight)
	}
	shrunk, err := BuildPrompt(batch, unit, facts, time.Unix(1_700_000_100, 0), PromptOptions{SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: tight,
	})
	if err != nil {
		t.Fatalf("tight BuildPrompt: %v", err)
	}
	if !strings.Contains(shrunk.User, "CONTEXT_MARKER") {
		t.Fatalf("tight prompt dropped conversation context before world floor; world-first shrink failed:\n%s", shrunk.User)
	}
	if !strings.Contains(shrunk.User, "NEW_MARKER") {
		t.Fatalf("tight prompt lost new message:\n%s", shrunk.User)
	}
	// Person facts are trimmed first; with 6→floor 3, at least one person-fact
	// letter must have disappeared while context stays.
	personCount := strings.Count(shrunk.User, "person-fact-")
	if personCount >= 6 {
		t.Fatalf("person facts were not trimmed under budget pressure: count=%d", personCount)
	}
	if personCount < worldFloor {
		t.Fatalf("person facts trimmed below floor: count=%d", personCount)
	}
}
