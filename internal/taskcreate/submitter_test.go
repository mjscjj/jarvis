package taskcreate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"jarvis/internal/contextpack"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type failingReadyNotifier struct {
	calls int
}

func (n *failingReadyNotifier) TaskReady(context.Context, uint64, int32) error {
	n.calls++
	return errors.New("notification unavailable")
}

func TestSubmitSucceedsAfterDurableCreateWhenNotificationFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.Task{}, &domain.TaskEvent{}); err != nil {
		t.Fatalf("migrate task tables: %v", err)
	}
	factory, err := NewFactory(db)
	if err != nil {
		t.Fatalf("NewFactory: %v", err)
	}
	submitter, err := NewSubmitter(factory)
	if err != nil {
		t.Fatalf("NewSubmitter: %v", err)
	}
	notifier := &failingReadyNotifier{}
	if err := submitter.SetNotifier(notifier); err != nil {
		t.Fatalf("SetNotifier: %v", err)
	}
	source, err := contextpack.Freeze(
		json.RawMessage(`{"instruction":"完成目标"}`),
		json.RawMessage(`{}`),
		"推进阻塞",
		nil,
	)
	if err != nil {
		t.Fatalf("freeze source: %v", err)
	}
	todoID := uint64(42)
	task, err := submitter.Submit(t.Context(), Input{
		TodoID: &todoID, Title: "推进阻塞", ActionType: "agent_task", Target: "完成目标",
		SourcePayload: source, SourceType: SourceTodo, ActorType: "system",
	})
	if err != nil {
		t.Fatalf("Submit returned notification failure after durable create: %v", err)
	}
	if task == nil || task.ID == 0 || task.Status != "pending" || notifier.calls != 1 {
		t.Fatalf("task=%+v notifier.calls=%d", task, notifier.calls)
	}
	var count int64
	if err := db.Model(&domain.Task{}).Count(&count).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("task count = %d, want 1", count)
	}
}
