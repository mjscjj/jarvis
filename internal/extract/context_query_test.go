package extract

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/contextpack"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
)

func TestTodoRevisionUsesOneSourceIdentityAndSearchesOriginal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "todo.db")), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Todo{}, &domain.TodoEvent{}); err != nil {
		t.Fatal(err)
	}
	candidate := strictCandidate()
	initial, _ := json.Marshal(candidate)
	old, err := contextpack.Freeze(initial, []byte(`{"messages":[{"message_id":"om_1","content":"请修改鉴权逻辑"}]}`), "old", nil)
	if err != nil {
		t.Fatal(err)
	}
	row := domain.Todo{Title: candidate.Title, Description: candidate.Payload, ActionType: candidate.ActionType, Target: candidate.Target, Status: "observing", Revision: 1, SourceMessageIDs: datatypes.JSON(`["om_1"]`), Content: datatypes.JSON(old), DedupFingerprint: "fixture"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	candidate.SourceMessageIDs = []string{"om_new"}
	candidate.TriggerMessageID = "om_new"
	candidate.SourceQuote = "新请求"
	source, _ := json.Marshal(candidate)
	flat, err := contextpack.Freeze(source, []byte(`{"messages":[{"message_id":"om_new","content":"新请求 only-in-original-789"}]}`), "brief", nil)
	if err != nil {
		t.Fatal(err)
	}
	writer := &PipelineStore{db: db}
	prepared := &preparedCandidate{Candidate: candidate, Content: datatypes.JSON(flat), LastEvidenceAt: time.Now()}
	if err := writer.updateTodo(db, &row, prepared, "test"); err != nil {
		t.Fatal(err)
	}
	var updated domain.Todo
	if err := db.First(&updated, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if string(updated.SourceMessageIDs) != string(contextpack.SourceMessageIDs(updated.Content)) {
		t.Fatal("indexed IDs diverged from current frozen source")
	}
	reader, err := NewTodoStore(db)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reader.ListTodos(t.Context(), TodoListFilter{Query: "only-in-original-789", SourceMessageID: "om_new", Page: 1, PageSize: 20})
	if err != nil || result.Total != 1 {
		t.Fatalf("raw message search: %#v %v", result, err)
	}
	// The revision still records the ordinary admission status transition.
	var event domain.TodoEvent
	if err := db.Where("todo_id = ?", row.ID).First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.ToStatus != "extracted" {
		t.Fatalf("status transition lost: %#v", event)
	}
}
