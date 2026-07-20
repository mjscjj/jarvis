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
	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/execute"
	"jarvis/internal/extract"
	"jarvis/internal/store"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

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
		service, err := NewService(tx, nil, nil)
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
		// Task.Background is the M3-frozen context_snapshot reused verbatim by M4.
		background, err := contextsnap.Decode(storedTask.Background)
		if err != nil {
			t.Fatalf("decode Task background snapshot: %v", err)
		}
		if len(background.Messages) != 1 || len(background.Memories) != 1 {
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
		service, err := NewService(tx, nil, nil)
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

	t.Run("MVP routes Todo through confirmation and manual Task completion", func(t *testing.T) {
		tx := beginRollbackTransaction(t, db)
		todo := createConfirmationFixture(t, tx, "extracted", time.Now().UnixNano())
		fixtureFingerprints = append(fixtureFingerprints, todo.DedupFingerprint)
		if err := tx.Model(&domain.Todo{}).
			Where("status = ? AND id <> ?", "extracted", todo.ID).
			Update("status", "dismissed").Error; err != nil {
			t.Fatalf("isolate existing extracted Todos: %v", err)
		}
		source, err := NewEvaluationSource(tx)
		if err != nil {
			t.Fatalf("NewEvaluationSource() error = %v", err)
		}
		evaluationStore, err := NewEvaluationStore(tx)
		if err != nil {
			t.Fatalf("NewEvaluationStore() error = %v", err)
		}
		worker, err := NewDecisionWorker(source, ManualGateEvaluator{}, evaluationStore, WorkerOptions{BatchLimit: 10})
		if err != nil {
			t.Fatalf("NewDecisionWorker() error = %v", err)
		}
		stats, err := worker.EvaluateOnce(context.Background())
		if err != nil {
			t.Fatalf("EvaluateOnce() error = %v", err)
		}
		if stats.Loaded != 1 || stats.Evaluated != 1 || stats.NeedDecision != 1 {
			t.Fatalf("Decision worker stats = %#v", stats)
		}
		var storedTodo domain.Todo
		if err := tx.First(&storedTodo, todo.ID).Error; err != nil {
			t.Fatalf("load evaluated Todo: %v", err)
		}
		if storedTodo.Status != RouteNeedDecision || storedTodo.Route == nil || *storedTodo.Route != RouteNeedDecision || storedTodo.Confidence != nil || storedTodo.Risk != nil {
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
		if audit.DecisionEngine != DecisionEngineManual || audit.FinalStatus != RouteNeedDecision || audit.TaskID != nil || audit.ConfidenceEff != nil || audit.RiskEff != nil {
			t.Fatalf("evaluation audit = %#v", audit)
		}
		todoReader, err := extract.NewTodoStore(tx)
		if err != nil {
			t.Fatalf("extract.NewTodoStore() error = %v", err)
		}
		detailStore, err := NewConfirmationDetailStore(tx, todoReader)
		if err != nil {
			t.Fatalf("NewConfirmationDetailStore() error = %v", err)
		}
		detail, err := detailStore.GetConfirmation(context.Background(), todo.ID)
		if err != nil {
			t.Fatalf("GetConfirmation() error = %v", err)
		}
		if detail.Todo.ID != todo.ID || detail.Todo.Status != RouteNeedDecision || len(detail.SourceMessages) != 1 || len(detail.Events) != 1 || len(detail.Audits) != 1 {
			t.Fatalf("confirmation detail = %#v", detail)
		}
		if detail.Assigner == nil || detail.Assigner.Name == nil || *detail.Assigner.Name != "Synthetic assigner" {
			t.Fatalf("confirmation assigner = %#v", detail.Assigner)
		}
		if detail.ProposedPlan != nil {
			t.Fatalf("confirmation proposed plan = %#v", detail.ProposedPlan)
		}

		confirmationService, err := NewService(tx, nil, nil)
		if err != nil {
			t.Fatalf("NewService() error = %v", err)
		}
		task, err := confirmationService.Approve(context.Background(), ApproveInput{
			TodoID: todo.ID, ExpectedVersion: 1, Plan: json.RawMessage(`{"steps":["review synthetic Todo"]}`), Channel: "backend",
		})
		if err != nil {
			t.Fatalf("Approve() error = %v", err)
		}
		if task.TodoID != todo.ID || task.Status != "pending" {
			t.Fatalf("approved Task = %#v", task)
		}
		taskBackground, err := contextsnap.Decode(task.Background)
		if err != nil {
			t.Fatalf("decode MVP Task background snapshot: %v", err)
		}
		if len(taskBackground.Messages) != 1 || len(taskBackground.Memories) != 1 {
			t.Fatalf("MVP Task background = %#v", taskBackground)
		}
		executionStore, err := execute.NewStore(tx)
		if err != nil {
			t.Fatalf("execute.NewStore() error = %v", err)
		}
		pendingTasks, err := executionStore.ListTasks(context.Background(), execute.TaskFilter{
			Statuses: []string{"pending"}, Page: 1, PageSize: 20,
		})
		if err != nil {
			t.Fatalf("ListTasks() error = %v", err)
		}
		if pendingTasks.Total != 1 || len(pendingTasks.Items) != 1 || pendingTasks.Items[0].ID != task.ID {
			t.Fatalf("pending Tasks = %#v", pendingTasks)
		}
		finishedTask, err := executionStore.Finish(context.Background(), execute.FinishInput{
			TaskID: task.ID, ExpectedVersion: 0, Status: "done",
			Result: json.RawMessage(`{"summary":"synthetic task completed manually"}`),
		})
		if err != nil {
			t.Fatalf("Finish() error = %v", err)
		}
		if finishedTask.Status != "done" || finishedTask.Version != 1 {
			t.Fatalf("finished Task = %#v", finishedTask)
		}
		_, err = executionStore.Finish(context.Background(), execute.FinishInput{
			TaskID: task.ID, ExpectedVersion: 0, Status: "done", Result: json.RawMessage(`{"summary":"duplicate"}`),
		})
		if !errors.Is(err, execute.ErrVersionConflict) {
			t.Fatalf("stale Finish() error = %v", err)
		}
		if err := tx.First(&storedTodo, todo.ID).Error; err != nil {
			t.Fatalf("reload confirmed Todo: %v", err)
		}
		if storedTodo.Status != "confirmed" || storedTodo.Version != 2 {
			t.Fatalf("confirmed Todo = %#v", storedTodo)
		}
		for model, want := range map[any]int64{&domain.Task{}: 1, &domain.TodoEvent{}: 2, &domain.DecisionAudit{}: 2} {
			var count int64
			if err := tx.Model(model).Where("todo_id = ?", todo.ID).Count(&count).Error; err != nil {
				t.Fatalf("count MVP artifacts: %v", err)
			}
			if count != want {
				t.Fatalf("MVP artifact %T count=%d, want %d", model, count, want)
			}
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
	snapshotRaw, err := contextsnap.Snapshot{
		SnapshotVersion: contextsnap.SnapshotVersion,
		CapturedAt:      now.Format(time.RFC3339),
		Group:           &contextsnap.Group{ID: group.ID, ChatID: group.ChatID, Name: &groupName},
		Assigner:        &contextsnap.Assigner{OpenID: assignerID, Name: &person.Name},
		Messages: []contextsnap.Message{{
			MessageID: messageID, ChatID: group.ChatID, SenderOpenID: assignerID,
			SenderName: person.Name, Content: message.Content, CreateTime: message.CreateTime,
		}},
		Memories: []map[string]any{{"memory": "synthetic project context", "score": 0.9}},
	}.Encode()
	if err != nil {
		t.Fatalf("build fixture context_snapshot: %v", err)
	}
	todo := domain.Todo{
		Title: "Confirm synthetic follow-up", Description: "Synthetic integration fixture", ActionType: "reply",
		Target: fmt.Sprintf("reply in %s", group.ChatID), Context: "synthetic reply context",
		OpenQuestions: datatypes.JSON([]byte(`[]`)), CommitmentStrength: "explicit",
		SourceMessageIDs: datatypes.JSON([]byte(fmt.Sprintf(`[%q]`, messageID))), SourceQuote: message.Content,
		GroupID: &group.ID, ProjectID: &project.ID, AssignerOpenID: &assignerID, Status: status, Route: &route,
		ContextSnapshot:  datatypes.JSON(snapshotRaw),
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
