package delegation

import (
	"encoding/json"
	"errors"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, *gorm.DB, *domain.Todo) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Todo{}, &domain.Task{}, &domain.TodoEvent{}, &domain.DelegationProgress{}); err != nil {
		t.Fatal(err)
	}
	todo := &domain.Todo{Title: "张三交方案", Description: "明确交办", ActionType: ActionType, Target: "发布方案", Status: "extracted", SourceMessageIDs: datatypes.JSON(`["om_source"]`), SourceQuote: "张三周五交方案", Content: datatypes.JSON(`{"source":{"payload":"原始理解"},"capture":{"messages":[{"message_id":"om_source","content":"前序约束与原话","mentions":[{"id":"ou_zhangsan"}]}]},"annotation":{"background":"完整背景"}}`), FirstSeenAt: time.Now(), LastEvidenceAt: time.Now()}
	if err := db.Create(todo).Error; err != nil {
		t.Fatal(err)
	}
	s, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); sqlDB.Close() })
	return s, db, todo
}
func intp(n int32) *int32 { return &n }
func boolp(b bool) *bool  { return &b }

func TestTodoExistsBeforeCheckAndTaskOutcomeDoesNotCloseIt(t *testing.T) {
	s, db, todo := fixture(t)
	page, err := s.List(t.Context(), Filter{Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].ID != todo.ID || page.Items[0].Version != 0 {
		t.Fatalf("before M5: %+v", page)
	}
	checks, err := s.Tasks(t.Context(), todo.ID, 1, 20)
	if err != nil || len(checks) != 0 {
		t.Fatalf("checks=%v err=%v", checks, err)
	}
	for _, status := range []string{"done", "failed", "waiting"} {
		task := domain.Task{TodoID: &todo.ID, Title: "本次核验", ActionType: ActionType, Target: "核验", SourceType: "todo", Status: status, SourcePayload: todo.Content}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		v, err := s.Get(t.Context(), todo.ID)
		if err != nil {
			t.Fatal(err)
		}
		if v.ClosedAt != nil || v.Version != 0 {
			t.Fatalf("Task %s changed progress: %+v", status, v)
		}
		db.Delete(&task)
	}
	updated, err := s.Update(t.Context(), todo.ID, UpdateInput{ExpectedVersion: intp(0), Content: json.RawMessage(`{"summary":"已核验，尚未交付","owner":{"id":"ou_lisi"},"proof":{"ticket":9007199254740993}}`), Closed: boolp(false), Actor: "m5"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ClosedAt != nil || updated.Version != 1 || !strings.Contains(string(updated.Content), "9007199254740993") {
		t.Fatalf("updated=%+v", updated)
	}
	page, err = s.List(t.Context(), Filter{Query: "ou_lisi", Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Summary != "已核验，尚未交付" || len(page.Items[0].SourcePayload) != 0 || len(page.Items[0].Content) != 0 {
		t.Fatalf("preview=%+v", page)
	}
}

func TestProgressCASClosureAuditAndFrozenEvidence(t *testing.T) {
	s, db, todo := fixture(t)
	closed, err := s.Update(t.Context(), todo.ID, UpdateInput{ExpectedVersion: intp(0), Content: json.RawMessage(`{"summary":"方案已验收","evidence":["doc:123"]}`), Closed: boolp(true), Actor: "m5"})
	if err != nil {
		t.Fatal(err)
	}
	if closed.ClosedAt == nil {
		t.Fatal("not closed")
	}
	if _, err := s.Update(t.Context(), todo.ID, UpdateInput{ExpectedVersion: intp(0), Content: json.RawMessage(`"stale"`), Actor: "user"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale err=%v", err)
	}
	reopened, err := s.Update(t.Context(), todo.ID, UpdateInput{ExpectedVersion: intp(1), Content: json.RawMessage(`["验收被撤回",{"unknown":true}]`), Closed: boolp(false), Actor: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ClosedAt != nil || reopened.Version != 2 {
		t.Fatalf("reopened=%+v", reopened)
	}
	var original domain.Todo
	db.First(&original, todo.ID)
	if original.Status != todo.Status || original.Version != todo.Version || string(original.Content) != string(todo.Content) {
		t.Fatal("progress mutated M3 evidence/lifecycle")
	}
	var events []domain.TodoEvent
	db.Where("todo_id = ?", todo.ID).Order("id").Find(&events)
	if len(events) != 2 || !strings.Contains(string(events[0].Detail), "doc:123") || !strings.Contains(string(events[1].Detail), "验收被撤回") {
		t.Fatalf("audit=%+v", events)
	}
}

func TestCheckHistoryUsesStableTodoIdentityAcrossSources(t *testing.T) {
	s, db, todo := fixture(t)
	tasks := []domain.Task{
		{TodoID: &todo.ID, Title: "首次", SourceType: "todo", ActionType: ActionType, Status: "done", SourcePayload: todo.Content},
		{Title: "复查", SourceType: "proactive", ActionType: "investigate", Status: "pending", SourcePayload: datatypes.JSON(`{"source":{"delegation_id":1},"capture":{},"annotation":{}}`)},
		{Title: "新证据", SourceType: "todo", ActionType: "investigate", Status: "failed", SourcePayload: datatypes.JSON(`{"source":{},"capture":{},"annotation":{"delegation_id":1}}`)},
		{Title: "无关", SourceType: "manual", ActionType: "investigate", Status: "pending", SourcePayload: datatypes.JSON(`{"source":{"delegation_id":99}}`)},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	checks, err := s.Tasks(t.Context(), todo.ID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 || checks[0].Title != "新证据" {
		t.Fatalf("checks=%+v", checks)
	}
	checks, err = s.Tasks(t.Context(), todo.ID, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].Title != "首次" {
		t.Fatalf("next page=%+v", checks)
	}
}
