package execute

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWaitingClaimRejectsOldRunDuplicateAndStaleClose(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Task{}, &domain.ExecutionRun{}, &domain.TaskEvent{}); err != nil {
		t.Fatal(err)
	}
	task := domain.Task{Title: "wait", ActionType: "investigate", SourceType: "manual", SourcePayload: datatypes.JSON(`{}`), Status: "waiting"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	session := "session"
	runs := []domain.ExecutionRun{
		{TaskID: task.ID, ActionType: task.ActionType, Stage: "execute", Sandbox: "read-only", Status: "waiting", Prompt: "first", CodexSessionID: &session, StartedAt: time.Now()},
		{TaskID: task.ID, ActionType: task.ActionType, Stage: "execute", Sandbox: "read-only", Status: "waiting", Prompt: "second", CodexSessionID: &session, StartedAt: time.Now()},
	}
	if err := db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWaiting(t.Context(), task.ID, runs[0].ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("old source run was accepted: %v", err)
	}
	if _, err := store.ClaimWaiting(t.Context(), task.ID, runs[1].ID); err != nil {
		t.Fatalf("current source run: %v", err)
	}
	if _, err := store.ClaimWaiting(t.Context(), task.ID, runs[1].ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("duplicate source run was accepted: %v", err)
	}
	if _, err := store.Close(t.Context(), CloseInput{TaskID: task.ID, ExpectedVersion: task.Version, ActorType: "proactive",
		Result: []byte(`{"summary":"stale decision"}`)}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale close was accepted: %v", err)
	}
}
