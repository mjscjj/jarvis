package contextsnap

import (
	"encoding/json"
	"testing"
)

func TestSnapshotEncodeDecodeRoundTrip(t *testing.T) {
	name := "Agent Runtime 攻坚群"
	snap := Snapshot{
		SnapshotVersion: SnapshotVersion,
		CapturedAt:      "2026-07-19T00:00:00Z",
		Principal:       &Principal{OpenID: "ou_me", Name: "chujiejie"},
		Group:           &Group{ID: 1, ChatID: "oc_x", Name: &name},
		Messages:        []Message{{MessageID: "m1", ChatID: "oc_x", Content: "读下 agent loop 代码"}},
	}
	raw, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Principal == nil || got.Principal.OpenID != "ou_me" {
		t.Fatalf("principal round-trip mismatch: %+v", got.Principal)
	}
	if len(got.Messages) != 1 || got.Messages[0].MessageID != "m1" {
		t.Fatalf("messages round-trip mismatch: %+v", got.Messages)
	}
}

func TestSnapshotEncodeRejectsEmpty(t *testing.T) {
	if _, err := (Snapshot{SnapshotVersion: SnapshotVersion}).Encode(); err == nil {
		t.Fatal("Encode() expected error for empty snapshot, got nil")
	}
}

func TestDecodeRejectsEmptyAndBadVersion(t *testing.T) {
	if _, err := Decode(nil); err == nil {
		t.Fatal("Decode(nil) expected error, got nil")
	}
	if _, err := Decode([]byte("null")); err == nil {
		t.Fatal("Decode(null) expected error, got nil")
	}
	bad, _ := json.Marshal(Snapshot{SnapshotVersion: "v0", Principal: &Principal{OpenID: "x"}})
	if _, err := Decode(bad); err == nil {
		t.Fatal("Decode(bad version) expected error, got nil")
	}
}

func TestSnapshotRepoPathProjectsFirstConfiguredWorkingCopy(t *testing.T) {
	snapshot := &Snapshot{Project: &Project{Repos: json.RawMessage(`[{"name":"jarvis","local_path":"  jarvis  "},{"local_path":"other"}]`)}}
	path, err := snapshot.RepoPath()
	if err != nil {
		t.Fatalf("RepoPath() error = %v", err)
	}
	if path == nil || *path != "jarvis" {
		t.Fatalf("RepoPath() = %v, want jarvis", path)
	}
}

func TestSnapshotRepoPathRejectsMalformedRepositories(t *testing.T) {
	snapshot := &Snapshot{Project: &Project{Repos: json.RawMessage(`{"local_path":"jarvis"}`)}}
	if _, err := snapshot.RepoPath(); err == nil {
		t.Fatal("RepoPath() accepted non-array project repos")
	}
}

func TestResolutionEncodeValidation(t *testing.T) {
	pid := uint64(45)
	valid := Resolution{Method: MethodCodexCLI, ProjectID: &pid, Confidence: 0.8, Basis: "codex 查群公告确认"}
	if _, err := valid.Encode(); err != nil {
		t.Fatalf("valid resolution Encode() error = %v", err)
	}
	unresolved := Resolution{Method: MethodUnresolved, Confidence: 0, Basis: "无法确定项目"}
	if _, err := unresolved.Encode(); err != nil {
		t.Fatalf("unresolved resolution Encode() error = %v", err)
	}
	if _, err := (Resolution{Method: "bogus", Confidence: 0.5, Basis: "x", ProjectID: &pid}).Encode(); err == nil {
		t.Fatal("Encode() expected error for invalid method, got nil")
	}
	if _, err := (Resolution{Method: MethodGroupBound, Confidence: 0.5, Basis: "x"}).Encode(); err == nil {
		t.Fatal("Encode() expected error when project_id missing for non-unresolved, got nil")
	}
}
