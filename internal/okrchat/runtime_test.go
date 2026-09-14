package okrchat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/chat"
	"jarvis/internal/okrworkspace/moduleconfig"
)

func TestDeleteSessionRemovesOnlyItsNativeState(t *testing.T) {
	r := &Runtime{root: t.TempDir()}
	dir := filepath.Join(r.root, "sessions", "cs_abc123", "native")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteSession("../ordinary"); err == nil {
		t.Fatal("invalid deletion accepted")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("invalid deletion changed state")
	}
	if err := r.DeleteSession("cs_abc123"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("native history remains: %v", err)
	}
}

func TestDockerBoundaryAndResume(t *testing.T) {
	r := &Runtime{root: "/state", repo: "/repo", socket: "/socket/access.sock", auth: "/login/auth.json", config: moduleconfig.ChatConfig{Image: "okr:test", Model: "test-model", ReasoningEffort: "medium"}}
	args := strings.Join(r.args(chat.Request{ThreadID: "native-thread", ImagePaths: []string{"/attachments/a.png"}}, "test-container", "/state/sessions/cs_a/native", "/state/sessions/cs_a/work"), " ")
	for _, required := range []string{"--network none", "--read-only", "--cap-drop ALL", "no-new-privileges", "src=/repo/scripts,dst=/opt/jarvis/scripts,readonly", "src=/repo/.agents/skills,dst=/opt/jarvis/.agents/skills,readonly", "src=/repo/web,dst=/opt/jarvis/web", "src=/socket/access.sock", "exec resume native-thread", "--image /attachments/a.png"} {
		if !strings.Contains(args, required) {
			t.Errorf("missing %s: %s", required, args)
		}
	}
	for _, denied := range []string{"docker.sock", "--network host", "src=/repo,dst=", "src=/state,dst=", "src=/repo/web,dst=/opt/jarvis/web,readonly", "chat.db", "--privileged", "--env-file"} {
		if strings.Contains(args, denied) {
			t.Errorf("unsafe mount/flag %s", denied)
		}
	}
}

func TestPrepareOnlyMapsOKRAttachments(t *testing.T) {
	r := &Runtime{root: t.TempDir()}
	input := chat.Request{SessionID: "cs_abc123", AttachmentPaths: []string{filepath.Join(r.root, "files", "a.txt")}, VisibleHistory: filepath.Join(r.root, "files", "a.txt")}
	req, err := r.Prepare(input)
	if err != nil {
		t.Fatal(err)
	}
	if req.AttachmentPaths[0] != "/attachments/a.txt" || req.VisibleHistory != "/attachments/a.txt" {
		t.Fatalf("bad mapping: %+v", req)
	}
	input.AttachmentPaths = []string{filepath.Join(r.root, "chat.db")}
	if _, err := r.Prepare(input); err == nil {
		t.Fatal("accepted DB attachment")
	}
	input.SessionID = "../ordinary-chat"
	if _, err := r.Prepare(input); err == nil {
		t.Fatal("accepted traversal session")
	}
}
