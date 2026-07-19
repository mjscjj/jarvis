package extract

import (
	"testing"

	"jarvis/internal/contextsnap"
)

func TestResolveProjectGroupBoundWins(t *testing.T) {
	bound := uint64(45)
	batch := ChatBatch{
		Group:   GroupContext{ID: 1, ChatID: "oc_x", ProjectID: &bound},
		Project: &ProjectContext{ID: 45, Code: "agent-runtime", Name: "Agent Runtime"},
	}
	hint := "some-other"
	id, res := resolveProject(batch, Candidate{ProjectHint: &hint})
	if id == nil || *id != 45 {
		t.Fatalf("expected group-bound project 45, got %v", id)
	}
	if res.Method != contextsnap.MethodGroupBound {
		t.Fatalf("expected method group_bound, got %q", res.Method)
	}
}

func TestResolveProjectHintMatchByCode(t *testing.T) {
	batch := ChatBatch{
		Group: GroupContext{ID: 1, ChatID: "oc_x"}, // unbound
		OtherProjects: []OtherProjectContext{
			{ID: 45, Code: "agent-runtime", Name: "Agent Runtime"},
			{ID: 46, Code: "skill-governance", Name: "Skill 管理与治理"},
		},
	}
	hint := "Agent-Runtime" // case-insensitive code match
	id, res := resolveProject(batch, Candidate{ProjectHint: &hint})
	if id == nil || *id != 45 {
		t.Fatalf("expected hint match project 45, got %v", id)
	}
	if res.Method != contextsnap.MethodProjectHint {
		t.Fatalf("expected method project_hint, got %q", res.Method)
	}
}

func TestResolveProjectHintMatchByName(t *testing.T) {
	batch := ChatBatch{
		Group:         GroupContext{ID: 1, ChatID: "oc_x"},
		OtherProjects: []OtherProjectContext{{ID: 46, Code: "skill-governance", Name: "Skill 管理与治理"}},
	}
	hint := "skill 管理与治理"
	id, _ := resolveProject(batch, Candidate{ProjectHint: &hint})
	if id == nil || *id != 46 {
		t.Fatalf("expected name match project 46, got %v", id)
	}
}

func TestResolveProjectUnresolved(t *testing.T) {
	batch := ChatBatch{
		Group:         GroupContext{ID: 1, ChatID: "oc_x"},
		OtherProjects: []OtherProjectContext{{ID: 46, Code: "skill-governance", Name: "Skill 管理与治理"}},
	}
	hint := "nonexistent-project"
	id, res := resolveProject(batch, Candidate{ProjectHint: &hint})
	if id != nil {
		t.Fatalf("expected unresolved (nil project), got %v", id)
	}
	if res.Method != contextsnap.MethodUnresolved {
		t.Fatalf("expected method unresolved, got %q", res.Method)
	}
	if _, err := res.Encode(); err != nil {
		t.Fatalf("unresolved resolution must encode: %v", err)
	}
}

func TestResolveProjectNoHintUnresolved(t *testing.T) {
	batch := ChatBatch{Group: GroupContext{ID: 1, ChatID: "oc_x"}}
	id, res := resolveProject(batch, Candidate{})
	if id != nil || res.Method != contextsnap.MethodUnresolved {
		t.Fatalf("expected unresolved with no hint, got id=%v method=%q", id, res.Method)
	}
}
