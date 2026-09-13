package extract

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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
		WorldOverview: json.RawMessage(`{"project":"proj-a","count":3}`),
		Group:         GroupContext{ChatID: "oc_1"},
		OtherProjects: otherProjects,
	}
	full, err := BuildPrompt(batch, unit, time.Unix(1_700_000_100, 0), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
		PrincipalOpenID: "ou_me", Location: time.UTC, MaxChars: 200_000,
	})
	if err != nil {
		t.Fatalf("full BuildPrompt: %v", err)
	}
	if !strings.Contains(full.User, "CONTEXT_MARKER") || !strings.Contains(full.User, "NEW_MARKER") {
		t.Fatalf("full prompt missing conversation markers")
	}
	if !strings.Contains(full.User, "proj-a") || !strings.Contains(full.User, `"count":3`) {
		t.Fatalf("full prompt missing world sections:\n%s", full.User)
	}

	tight := len([]rune(full.System)) + len([]rune(full.User)) - 80
	if tight < 2000 {
		t.Fatalf("unexpected full prompt size %d", tight)
	}
	shrunk, err := BuildPrompt(batch, unit, time.Unix(1_700_000_100, 0), PromptOptions{InitiativeLevel: "normal", SystemPrompt: testM3SystemPrompt,
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
	if !strings.Contains(shrunk.User, "proj-a") || !strings.Contains(shrunk.User, `"count":3`) {
		t.Fatalf("tight prompt trimmed world data:\n%s", shrunk.User)
	}
}
