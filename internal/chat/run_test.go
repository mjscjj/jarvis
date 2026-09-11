package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
)

func TestSessionRunningAndRejectedInputRemainIntact(t *testing.T) {
	svc := newPersistentTestService(t)
	session, err := svc.CreateSession(t.Context(), CreateSessionInput{Agent: "codex", Model: "gpt-5.5", ReasoningEffort: "high", Draft: json.RawMessage(`{"text":"keep draft"}`)})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	svc.active[session.ID] = cancel
	view, err := svc.GetSession(t.Context(), session.ID)
	if err != nil || !view.Running {
		t.Fatalf("running view: %+v, %v", view, err)
	}
	list, err := svc.ListSessions(t.Context(), "", false)
	if err != nil || len(list) != 1 || !list[0].Running {
		t.Fatalf("running list: %+v, %v", list, err)
	}
	err = svc.StreamSession(t.Context(), session.ID, SendInput{Message: "new input"}, func(Event) error {
		t.Fatal("rejected input must not be acknowledged")
		return nil
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	view, err = svc.GetSession(t.Context(), session.ID)
	if err != nil || len(view.Messages) != 0 || string(view.Draft) != `{"text":"keep draft"}` {
		t.Fatalf("rejected input changed session: %+v, %v", view, err)
	}
	if !svc.CancelSession(session.ID) || runCtx.Err() != context.Canceled {
		t.Fatal("cancel did not reach the active reply")
	}
	delete(svc.active, session.ID) // Runner cleanup removes the in-memory state.
	view, err = svc.GetSession(t.Context(), session.ID)
	if err != nil || view.Running {
		t.Fatalf("completed view: %+v, %v", view, err)
	}
}

func TestReplyPersistsBeforeDeliveryAndRecordsInterruption(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprintf("interrupted=%v", interrupted), func(t *testing.T) {
			binDir := t.TempDir()
			script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '%s\\n' 'codex-cli 0.153.4'; exit 0; fi\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"native-test\"}' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"saved reply\"}}'\n"
			if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			svc := newPersistentTestService(t)
			session, err := svc.CreateSession(t.Context(), CreateSessionInput{Agent: "codex", Model: "test", ReasoningEffort: "high"})
			if err != nil {
				t.Fatal(err)
			}
			err = svc.StreamSession(t.Context(), session.ID, SendInput{Message: "hello"}, func(event Event) error {
				if event.Kind != EventDelta {
					return nil
				}
				view, err := svc.GetSession(t.Context(), session.ID)
				if err != nil || len(view.Messages) != 2 || view.Messages[1].Text != "saved reply" || view.Messages[1].Status != "streaming" {
					t.Fatalf("reply not persisted before delivery: %+v, %v", view, err)
				}
				if interrupted {
					if !svc.CancelSession(session.ID) {
						t.Fatal("active reply was not canceled")
					}
				}
				return nil
			})
			if interrupted && !errors.Is(err, context.Canceled) || !interrupted && err != nil {
				t.Fatalf("run: %v", err)
			}
			view, err := svc.GetSession(t.Context(), session.ID)
			want := "completed"
			if interrupted {
				want = "interrupted"
			}
			if err != nil || view.Running || view.Messages[1].Text != "saved reply" || view.Messages[1].Status != want {
				t.Fatalf("final reply: %+v, %v", view, err)
			}
		})
	}
}

func TestOrphanedStreamingReplyIsShownAsInterrupted(t *testing.T) {
	svc := newPersistentTestService(t)
	session, err := svc.CreateSession(t.Context(), CreateSessionInput{Agent: "codex", Model: "test", ReasoningEffort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	row := domain.ChatMessage{ID: "reply", SessionID: session.ID, Role: "assistant", Text: "saved before restart", Meta: datatypes.JSON(`{"status":"streaming"}`)}
	if err := svc.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	view, err := svc.GetSession(t.Context(), session.ID)
	if err != nil || view.Running || view.Messages[0].Status != "interrupted" || view.Messages[0].Text != row.Text {
		t.Fatalf("restarted view: %+v, %v", view, err)
	}
	// A later turn must not make the older interrupted reply look live again.
	svc.active[session.ID] = func() {}
	next := domain.ChatMessage{ID: "next-reply", SessionID: session.ID, Role: "assistant", Meta: datatypes.JSON(`{"status":"streaming"}`)}
	if err := svc.db.Create(&next).Error; err != nil {
		t.Fatal(err)
	}
	view, err = svc.GetSession(t.Context(), session.ID)
	if err != nil || view.Messages[0].Status != "interrupted" || view.Messages[1].Status != "streaming" {
		t.Fatalf("later turn: %+v, %v", view, err)
	}
}

func TestAcceptedEventFollowsMessageAndAttachmentPersistence(t *testing.T) {
	installChatCLI(t, `printf '%s\n' '{"type":"thread.started","thread_id":"thread-1"}' '{"type":"item.delta","delta":"done"}'`)
	svc := newPersistentTestService(t)
	session, err := svc.CreateSession(t.Context(), CreateSessionInput{Agent: "codex", Model: "gpt-5.5", ReasoningEffort: "high", Draft: json.RawMessage(`{"text":"hello"}`)})
	if err != nil {
		t.Fatal(err)
	}
	file, err := svc.SaveUpload(t.Context(), session.ID, "note.txt", "text/plain", 5, func(path string) error { return os.WriteFile(path, []byte("hello"), 0o600) })
	if err != nil {
		t.Fatal(err)
	}
	accepted := false
	err = svc.StreamSession(t.Context(), session.ID, SendInput{Message: "hello", AttachmentIDs: []string{file.ID}}, func(event Event) error {
		if !accepted {
			if event.Kind != EventAccepted {
				t.Fatalf("first event = %s", event.Kind)
			}
			accepted = true
			view, err := svc.GetSession(t.Context(), session.ID)
			if err != nil || !view.Running || len(view.Messages) != 1 || view.Messages[0].Text != "hello" || len(view.Messages[0].Attachments) != 1 || len(view.PendingAttachments) != 0 || string(view.Draft) != "{}" {
				t.Fatalf("acceptance before persistence: %+v, %v", view, err)
			}
		}
		return nil
	})
	if !accepted || err != nil || svc.sessionRunning(session.ID) {
		t.Fatalf("acceptance/cleanup: %v, %v", accepted, err)
	}
	view, err := svc.GetSession(t.Context(), session.ID)
	if err != nil || len(view.Messages) != 2 || view.Messages[1].Text != "done" {
		t.Fatalf("persisted assistant reply: %+v, %v", view, err)
	}
}

func installChatCLI(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	writeExecutable(t, dir, "codex", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '%s\\n' 'codex-cli 0.153.4'; exit 0; fi\ncat >/dev/null\n"+body+"\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestClientDisconnectKeepsReplyAndFailureRecoverable(t *testing.T) {
	for _, disconnectAt := range []EventKind{EventAccepted, EventThread, EventDelta} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("disconnect=%s/fail=%v", disconnectAt, fail), func(t *testing.T) {
				script := "printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread-1\"}'\n"
				// Before the first delta, the failing CLI can terminate without any text.
				if !fail || disconnectAt == EventDelta {
					script += "printf '%s\\n' '{\"type\":\"item.delta\",\"delta\":\"后台\"}' '{\"type\":\"item.delta\",\"delta\":\"完成\"}'\n"
				}
				if fail {
					script += "printf '%s\\n' 'credentials expired' >&2\nexit 1\n"
				}
				installChatCLI(t, script)
				svc := newPersistentTestService(t)
				session, err := svc.CreateSession(t.Context(), CreateSessionInput{Agent: "codex", Model: "test", ReasoningEffort: "high"})
				if err != nil {
					t.Fatal(err)
				}
				ctx, disconnect := context.WithCancel(t.Context())
				defer disconnect()
				disconnected := false
				err = svc.StreamSession(ctx, session.ID, SendInput{Message: "hello"}, func(e Event) error {
					if disconnected {
						t.Fatal("attempted delivery after disconnection")
					}
					if e.Kind == disconnectAt {
						disconnected = true
						disconnect()
						return errors.New("browser closed")
					}
					return nil
				})
				if !disconnected || (err != nil) != fail {
					t.Fatalf("disconnected=%v, run error=%v", disconnected, err)
				}
				view, err := svc.GetSession(t.Context(), session.ID)
				if err != nil || view.Running || len(view.Messages) != 2 {
					t.Fatalf("recovered: %+v, %v", view, err)
				}
				reply := view.Messages[1]
				if fail {
					if reply.Status != "interrupted" || !strings.Contains(reply.Error, "credentials expired") {
						t.Fatalf("lost failure: %+v", reply)
					}
				} else if reply.Status != "completed" || reply.Text != "后台完成" || reply.Error != "" {
					t.Fatalf("lost reply: %+v", reply)
				}
				if string(view.Draft) != "{}" {
					t.Fatalf("stale draft: %s", view.Draft)
				}
			})
		}
	}
}
