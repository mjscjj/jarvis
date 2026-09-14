package worldview

import (
	"context"
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/domain"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOverviewCrossSourceBudgetAndPagination(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "world.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(domain.CoreModels()...); err != nil {
		t.Fatal(err)
	}
	summary := strings.Repeat("长背景", 2000)
	taskSummary := "TASK_PROGRESS_MUST_NOT_APPEAR"
	if err := db.Create(&domain.PrincipalProfile{OpenID: "owner", Name: "我", Summary: &summary}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		source := []string{"manual", "scheduled_task", "todo", "proactive"}[i%4]
		task := domain.Task{Title: fmt.Sprintf("事项%d", i), SourceType: source, Status: "pending", Summary: &taskSummary, ActionType: "investigate", Target: "index", SourcePayload: []byte(`{}`)}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	todo := domain.Todo{Title: "另一个线索", ActionType: "investigate", Target: "item", Status: "observing", Content: []byte(`{}`), SourceMessageIDs: []byte(`[]`), SourceQuote: "原始片段", Description: "M3_JUDGMENT_MUST_NOT_APPEAR", DedupFingerprint: "unique"}
	if err := db.Create(&todo).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.Task{}).Where("id = ?", 25).Update("todo_id", todo.ID).Error; err != nil {
		t.Fatal(err)
	}
	v, err := Read(context.Background(), db, Filter{TaskID: 25})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := v.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCount(raw) > Budget || strings.Contains(string(raw), "M3_JUDGMENT_MUST_NOT_APPEAR") || strings.Contains(string(raw), summary) {
		t.Fatalf("oversized or contaminated directory: %s", raw)
	}
	for _, section := range v.Sections {
		if section.Total > 0 && len(section.Items) == 0 {
			t.Fatalf("budget removed entire %s category: %+v", section.Name, section)
		}
	}
	sources := map[any]bool{}
	for _, s := range v.Sections {
		for _, row := range s.Items {
			if s.Name == "task" {
				sources[row["source_type"]] = true
				if _, exists := row["summary"]; exists {
					t.Fatalf("Task progress leaked into world directory: %+v", row)
				}
			}
			if s.Name == "todo" {
				t.Fatal("current source todo leaked")
			}
		}
	}
	if len(sources) != 4 {
		t.Fatalf("missing sources: %v", sources)
	}
	if strings.Contains(string(raw), taskSummary) {
		t.Fatalf("Task summary preview leaked into world directory: %s", raw)
	}
	page, err := Read(context.Background(), db, Filter{Section: "task", Offset: 10, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sections) != 1 || len(page.Sections[0].Items) != 3 || page.Sections[0].Total != 25 || page.Sections[0].NextOffset != 13 {
		t.Fatalf("pagination: %+v", page)
	}
	if page.Sections[0].ReadAt == "" {
		t.Fatal("section read time missing")
	}
	matched, err := Read(context.Background(), db, Filter{Section: "todo", Query: "原始片段", Limit: 5})
	if err != nil || len(matched.Sections) != 1 || matched.Sections[0].Total != 1 {
		t.Fatalf("summary/source search: %+v %v", matched, err)
	}
}

func TestOverviewBoundsUnrestrictedIndexText(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "world.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(domain.CoreModels()...); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("超长目录字段", 2000)
	if err := db.Create(&domain.PrincipalProfile{OpenID: huge, Name: huge, Title: &huge, Summary: &huge}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Task{Title: huge, SourceType: "manual", Status: "pending", ActionType: "investigate", Target: "index", SourcePayload: []byte(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}

	view, err := Read(context.Background(), db, Filter{})
	if err != nil {
		t.Fatalf("Read() rejected display-only text: %v", err)
	}
	raw, err := view.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCount(raw) > Budget {
		t.Fatalf("overview size = %d, want <= %d", utf8.RuneCount(raw), Budget)
	}
	if strings.Contains(string(raw), huge) {
		t.Fatal("unbounded index text leaked into overview")
	}
}
