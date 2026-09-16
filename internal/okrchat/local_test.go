package okrchat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/chat"
	"jarvis/internal/okrworkspace/moduleconfig"
)

type localPrompts struct{}

func (localPrompts) Content(context.Context, string) (string, error) { return "Local OKR", nil }

func TestLocalChatOwnsSessionsAttachmentsAndUsesInstanceCLI(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo 'codex-cli 0.154.0'; exit 0; fi
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"local-thread"}' '{"type":"item.completed","item":{"type":"agent_message","text":"LOCAL_OK"}}' '{"type":"turn.completed"}'
`
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	cfg := moduleconfig.ChatConfig{Enabled: true, Runtime: "local", Model: "test", ReasoningEffort: "medium", TimeoutSeconds: 10}
	root := t.TempDir()
	svc, closeDB, err := Open(t.Context(), cfg, root, "", "", "Emily", localPrompts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	owner := chat.WithOwner(t.Context(), "dev-owner")
	session, err := svc.CreateSession(owner, chat.CreateSessionInput{Agent: "codex", Model: "test", ReasoningEffort: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetSession(chat.WithOwner(t.Context(), "other-owner"), session.ID); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("session exposed to another owner: %v", err)
	}
	file, err := svc.SaveUpload(owner, session.ID, "note.txt", "text/plain", 4, func(path string) error {
		if !strings.HasPrefix(path, filepath.Join(root, "files")+string(os.PathSeparator)) {
			t.Fatalf("attachment escaped instance: %s", path)
		}
		return os.WriteFile(path, []byte("note"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetAttachment(chat.WithOwner(t.Context(), "other-owner"), file.ID); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("attachment exposed to another owner: %v", err)
	}
	if err := svc.StreamSession(owner, session.ID, chat.SendInput{Message: "hello"}, func(chat.Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	view, err := svc.GetSession(owner, session.ID)
	if err != nil || len(view.Messages) != 2 || view.Messages[1].Text != "LOCAL_OK" {
		t.Fatalf("local chat did not persist reply: %+v, %v", view, err)
	}
	other, closeOther, err := Open(t.Context(), cfg, t.TempDir(), "", "", "Emily", localPrompts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeOther()
	if sessions, err := other.ListSessions(owner, "", false); err != nil || len(sessions) != 0 {
		t.Fatalf("chat history crossed instances: %+v, %v", sessions, err)
	}
}
