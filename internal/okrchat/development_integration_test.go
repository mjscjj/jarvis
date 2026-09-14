package okrchat

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"

	"jarvis/internal/chat"
	"jarvis/internal/okrworkspace/moduleconfig"
)

// Explicit opt-in: one real model turn inside the running development instance.
// Does not write product data or send any Feishu message.
func TestDevelopmentModelCanInspectFullSource(t *testing.T) {
	root := os.Getenv("EMILY_DEVELOPMENT_CHAT_ROOT")
	if root == "" {
		t.Skip("set EMILY_DEVELOPMENT_CHAT_ROOT for a live runtime check")
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	id := "cs_" + hex.EncodeToString(nonce[:])
	r := &DevelopmentRuntime{container: "emily-development", docker: docker, root: root, cfg: moduleconfig.ChatConfig{Model: "gpt-5.6-sol", ReasoningEffort: "high", TimeoutSeconds: 120}}
	defer r.DeleteSession(id)
	var reply strings.Builder
	prompt := `Use a shell command to check that /opt/jarvis/go.mod, /opt/jarvis/web/package.json and /opt/jarvis/data/okr/okr.db exist, while /opt/jarvis/var/jarvis-lixiaolin.db and /var/run/docker.sock do not. Do not read credentials, database contents or any private files. Do not modify files or call external tools other than this shell check. If all checks pass, reply only EMILY_DEVELOPMENT_OK.`
	err = r.Stream(t.Context(), chat.Request{SessionID: id}, prompt, func(event chat.Event) error {
		if event.Kind == chat.EventDelta {
			reply.WriteString(event.Text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.String(), "EMILY_DEVELOPMENT_OK") {
		t.Fatalf("unexpected reply: %s", reply.String())
	}
}
