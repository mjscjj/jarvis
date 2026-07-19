package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/memory"
	"jarvis/internal/store"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type fixtureMemorySearcher struct {
	input memory.SearchInput
}

func (f *fixtureMemorySearcher) Search(_ context.Context, input memory.SearchInput) (*memory.SearchResponse, error) {
	f.input = input
	return &memory.SearchResponse{Results: []map[string]any{{"memory": "synthetic project context", "score": 0.9}}}, nil
}

// TestConfirmationTransactionLive uses only synthetic rows inside an outer
// transaction. It does not call Feishu, mem0, or a model and always rolls back.
func TestConfirmationTransactionLive(t *testing.T) {
	configPath := os.Getenv("JARVIS_TEST_DECIDE_CONFIG")
	if configPath == "" {
		t.Skip("JARVIS_TEST_DECIDE_CONFIG is required for confirmation integration test")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	db, err := store.OpenMySQL(context.Background(), cfg.MySQL)
	if err != nil {
		t.Fatalf("store.OpenMySQL() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(db); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	fixtureFingerprints := make([]string, 0, 3)

	t.Run("approve freezes Task and audit atomically", func(t *testing.T) {
		tx := beginRollbackTransaction(t, db)
		todo := createConfirmationFixture(t, tx, "need_decision", time.Now().UnixNano())
		fixtureFingerprints = append(fixtureFingerprints, todo.DedupFingerprint)
		memories := &fixtureMemorySearcher{}
		snapshotter, err := NewBackgroundSnapshotter(tx, memories, BackgroundOptions{MemoryTopK: 5, MemoryThreshold: 0.4})
		if err != nil {
			t.Fatalf("NewBackgroundSnapshotter() error = %v", err)
		}
		service, err := NewService(tx, snapshotter)
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}

		task, err := service.Approve(context.Background(), ApproveInput{
			TodoID: todo.ID, ExpectedVersion: 0, Plan: json.RawMessage(`{"steps":["draft","review"]}`), Channel: "backend",
		})
		if err != nil {
			t.Fatalf("Approve() error = %v", err)
		}
		if task.TodoID != todo.ID || task.Status != "pending" || task.ConfirmedBy != "user" || task.AutonomyMode != "copilot" || task.Version != 0 {
			t.Fatalf("Task = %#v", task)
		}
		if len(task.ActionHash) != 64 {
			t.Fatalf("action_hash = %q", task.ActionHash)
		}
		if got := memories.input.Filters["project_id"]; got != *todo.ProjectID {
			t.Fatalf("memory project filter = %#v, want %d", got, *todo.ProjectID)
		}

		var storedTodo domain.Todo
		if err := tx.First(&storedTodo, todo.ID).Error; err != nil {
			t.Fatalf("load confirmed Todo: %v", err)
		}
		if storedTodo.Status != "confirmed" || storedTodo.Version != 1 {
			t.Fatalf("stored Todo status=%q version=%d", storedTodo.Status, storedTodo.Version)
		}
		var storedTask domain.Task
		if err := tx.Where("todo_id = ?", todo.ID).First(&storedTask).Error; err != nil {
			t.Fatalf("load Task: %v", err)
		}
		if storedTask.ActionHash != task.ActionHash {
			t.Fatalf("stored action_hash = %q, response = %q", storedTask.ActionHash, task.ActionHash)
		}
		var background struct {
			TodoID   uint64 `json:"todo_id"`
			Messages []struct {
				MessageID string `json:"message_id"`
			} `json:"messages"`
			Memories []map[string]any `json:"memories"`
		}
		if err := json.Unmarshal(storedTask.Background, &background); err != nil {
			t.Fatalf("decode Task background: %v", err)
		}
		if background.TodoID != todo.ID || len(background.Messages) != 1 || len(background.Memories) != 1 {
			t.Fatalf("background = %#v", background)
		}
		assertDecisionArtifacts(t, tx, todo.ID, storedTask.ID, "confirmed")

		_, err = service.Approve(context.Background(), ApproveInput{
			TodoID: todo.ID, ExpectedVersion: 0, Plan: json.RawMessage(`{"steps":["draft"]}`), Channel: "backend",
		})
		if !errors.Is(err, ErrVersionConflict) {
			t.Fatalf("stale Approve() error = %v, want ErrVersionConflict", err)
		}
	})

	t.Run("reject dismisses Todo without Task", func(t *testing.T) {
		tx := beginRollbackTransaction(t, db)
		todo := createConfirmationFixture(t, tx, "need_info", time.Now().UnixNano())
		fixtureFingerprints = append(fixtureFingerprints, todo.DedupFingerprint)
		service, err := NewService(tx, backgroundSnapshotFunc(func(context.Context, *domain.Todo) (json.RawMessage, error) {
			return nil, fmt.Errorf("background must not be called for rejection")
		}))
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}
		result, err := service.Reject(context.Background(), RejectInput{
			TodoID: todo.ID, ExpectedVersion: 0, Reason: "synthetic duplicate", Channel: "backend",
		})
		if err != nil {
			t.Fatalf("Reject() error = %v", err)
		}
		if result.Status != "dismissed" || result.Version != 1 {
			t.Fatalf("Reject result = %#v", result)
		}
		var taskCount int64
		if err := tx.Model(&domain.Task{}).Where("todo_id = ?", todo.ID).Count(&taskCount).Error; err != nil {
			t.Fatalf("count Tasks: %v", err)
		}
		if taskCount != 0 {
			t.Fatalf("Task count = %d, want 0", taskCount)
		}
		assertDecisionArtifacts(t, tx, todo.ID, 0, "dismissed")
	})

	t.Run("evaluation routes Todo and audit atomically", func(t *testing.T) {
		tx := beginRollbackTransaction(t, db)
		todo := createConfirmationFixture(t, tx, "extracted", time.Now().UnixNano())
		fixtureFingerprints = append(fixtureFingerprints, todo.DedupFingerprint)
		store, err := NewEvaluationStore(tx)
		if err != nil {
			t.Fatalf("NewEvaluationStore() error = %v", err)
		}
		input := fixtureEvaluationInput()
		input.TodoID = todo.ID
		result, err := store.Apply(context.Background(), input)
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if result.Status != RouteNeedDecision || result.Version != 1 {
			t.Fatalf("Evaluation result = %#v", result)
		}
		var storedTodo domain.Todo
		if err := tx.First(&storedTodo, todo.ID).Error; err != nil {
			t.Fatalf("load evaluated Todo: %v", err)
		}
		if storedTodo.Status != RouteNeedDecision || storedTodo.Route == nil || *storedTodo.Route != RouteNeedDecision || storedTodo.Confidence == nil || *storedTodo.Confidence != input.Confidence {
			t.Fatalf("stored evaluated Todo = %#v", storedTodo)
		}
		var taskCount int64
		if err := tx.Model(&domain.Task{}).Where("todo_id = ?", todo.ID).Count(&taskCount).Error; err != nil {
			t.Fatalf("count evaluation Tasks: %v", err)
		}
		if taskCount != 0 {
			t.Fatalf("evaluation generated %d Tasks", taskCount)
		}
		var event domain.TodoEvent
		if err := tx.Where("todo_id = ? AND actor = ?", todo.ID, "m4").First(&event).Error; err != nil {
			t.Fatalf("load evaluation event: %v", err)
		}
		if event.ToStatus != RouteNeedDecision || event.FromStatus == nil || *event.FromStatus != "extracted" {
			t.Fatalf("evaluation event = %#v", event)
		}
		var audit domain.DecisionAudit
		if err := tx.Where("todo_id = ?", todo.ID).First(&audit).Error; err != nil {
			t.Fatalf("load evaluation audit: %v", err)
		}
		if audit.DecisionEngine != DecisionEngineRule || audit.FinalStatus != RouteNeedDecision || audit.TaskID != nil {
			t.Fatalf("evaluation audit = %#v", audit)
		}
	})

	var remaining int64
	if err := db.Model(&domain.Todo{}).Where("dedup_fingerprint IN ?", fixtureFingerprints).Count(&remaining).Error; err != nil {
		t.Fatalf("verify fixture rollback: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("synthetic Todo rows remaining after rollback = %d", remaining)
	}
}

type backgroundSnapshotFunc func(context.Context, *domain.Todo) (json.RawMessage, error)

func (f backgroundSnapshotFunc) Snapshot(ctx context.Context, todo *domain.Todo) (json.RawMessage, error) {
	return f(ctx, todo)
}

func beginRollbackTransaction(t *testing.T, db *gorm.DB) *gorm.DB {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() {
		if err := tx.Rollback().Error; err != nil && !errors.Is(err, gorm.ErrInvalidTransaction) {
			t.Errorf("rollback transaction: %v", err)
		}
	})
	return tx
}

func createConfirmationFixture(t *testing.T, tx *gorm.DB, status string, suffix int64) domain.Todo {
	t.Helper()
	code := fmt.Sprintf("decide-%d", suffix)
	project := domain.Project{Code: &code, Name: "Synthetic decision project", Role: "owner", Status: "active", Priority: 1}
	if err := tx.Create(&project).Error; err != nil {
		t.Fatalf("create Project: %v", err)
	}
	groupName := "Synthetic decision group"
	group := domain.Group{ChatID: fmt.Sprintf("oc_decide_%d", suffix), ChatMode: "group", Name: &groupName, ProjectID: &project.ID, Tier: "cold"}
	if err := tx.Create(&group).Error; err != nil {
		t.Fatalf("create Group: %v", err)
	}
	assignerID := fmt.Sprintf("ou_decide_%d", suffix)
	person := domain.Person{OpenID: assignerID, Name: "Synthetic assigner", Role: "colleague", PriorityWeight: 0.5, IsActive: true}
	if err := tx.Create(&person).Error; err != nil {
		t.Fatalf("create Person: %v", err)
	}
	messageID := fmt.Sprintf("om_decide_%d", suffix)
	message := domain.Message{
		MessageID: messageID, ChatID: group.ChatID, GroupID: &group.ID, ChatMode: "group",
		SenderOpenID: assignerID, SenderName: person.Name, SenderType: "user", MessageType: "text",
		Content: "synthetic confirmation fixture", CreateTime: time.Now().UnixMilli(), Source: "poll", RenderOK: true,
	}
	if err := tx.Create(&message).Error; err != nil {
		t.Fatalf("create Message: %v", err)
	}
	now := time.Now().UTC()
	route := status
	todo := domain.Todo{
		Title: "Confirm synthetic follow-up", Description: "Synthetic integration fixture", ActionType: "reply",
		Slots: datatypes.JSON([]byte(fmt.Sprintf(`{"chat_id":%q}`, group.ChatID))), CommitmentStrength: "explicit",
		SourceMessageIDs: datatypes.JSON([]byte(fmt.Sprintf(`[%q]`, messageID))), SourceQuote: message.Content,
		GroupID: &group.ID, ProjectID: &project.ID, AssignerOpenID: &assignerID, Status: status, Route: &route,
		DedupFingerprint: fmt.Sprintf("%064x", suffix), ExtractionModel: "synthetic", PromptVersion: "test-v1", Revision: 1,
		FirstSeenAt: now, LastEvidenceAt: now,
	}
	if err := tx.Create(&todo).Error; err != nil {
		t.Fatalf("create Todo: %v", err)
	}
	return todo
}

func assertDecisionArtifacts(t *testing.T, tx *gorm.DB, todoID, taskID uint64, finalStatus string) {
	t.Helper()
	var events []domain.TodoEvent
	if err := tx.Where("todo_id = ? AND actor = ?", todoID, "m4").Find(&events).Error; err != nil {
		t.Fatalf("load Todo events: %v", err)
	}
	if len(events) != 1 || events[0].ToStatus != finalStatus {
		t.Fatalf("Todo events = %#v", events)
	}
	var audits []domain.DecisionAudit
	if err := tx.Where("todo_id = ?", todoID).Find(&audits).Error; err != nil {
		t.Fatalf("load decision audits: %v", err)
	}
	if len(audits) != 1 || audits[0].FinalStatus != finalStatus || audits[0].DecisionEngine != "manual" {
		t.Fatalf("Decision audits = %#v", audits)
	}
	if taskID == 0 {
		if audits[0].TaskID != nil || audits[0].ActionHash != nil {
			t.Fatalf("reject audit unexpectedly references Task: %#v", audits[0])
		}
		return
	}
	if audits[0].TaskID == nil || *audits[0].TaskID != taskID || audits[0].ActionHash == nil {
		t.Fatalf("approval audit = %#v", audits[0])
	}
}
