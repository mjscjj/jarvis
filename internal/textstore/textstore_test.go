package textstore

import (
	"fmt"
	"testing"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeInputPreservesArbitraryText(t *testing.T) {
	input, err := normalizeInput(Input{
		StorageKey: " custom_prompt ",
		Name:       " 自定义提示词 ",
		Content:    " 第一行\n第二行 ",
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.StorageKey != "custom_prompt" || input.Name != "自定义提示词" || input.Content != "第一行\n第二行" {
		t.Fatalf("normalizeInput() = %#v", input)
	}
}

func TestNormalizeInputRequiresAllFields(t *testing.T) {
	for _, input := range []Input{
		{Name: "name", Content: "content"},
		{StorageKey: "key", Content: "content"},
		{StorageKey: "key", Name: "name"},
	} {
		if _, err := normalizeInput(input); err == nil {
			t.Fatalf("normalizeInput(%#v) must fail", input)
		}
	}
}

func TestSeedDefaultsCreatesEveryBuiltInRecord(t *testing.T) {
	service, db := newTestService(t)
	if err := service.SeedDefaults(t.Context()); err != nil {
		t.Fatalf("SeedDefaults() error = %v", err)
	}
	var rows []domain.TextStorage
	if err := db.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("list seeded rows: %v", err)
	}
	if len(rows) != len(defaultRecords()) {
		t.Fatalf("seeded rows = %d, want %d", len(rows), len(defaultRecords()))
	}
	for _, record := range defaultRecords() {
		content, err := service.Content(t.Context(), record.key)
		if err != nil {
			t.Fatalf("Content(%q) error = %v", record.key, err)
		}
		if content != record.content {
			t.Fatalf("Content(%q) was not seeded from its registered default", record.key)
		}
	}
}

func TestSeedDefaultsPreservesExistingAndDeletedRecords(t *testing.T) {
	service, db := newTestService(t)
	custom := domain.TextStorage{
		StorageKey: SystemPromptExecuteKey,
		Name:       "自定义直接执行提示词",
		Content:    "用户已经改过",
	}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatalf("create custom record: %v", err)
	}
	deleted := domain.TextStorage{
		StorageKey: SystemPromptResumeHumanKey,
		Name:       "已删除的人工恢复提示词",
		Content:    "不要自动恢复",
	}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatalf("create deleted record: %v", err)
	}
	if err := db.Delete(&deleted).Error; err != nil {
		t.Fatalf("soft delete record: %v", err)
	}

	if err := service.SeedDefaults(t.Context()); err != nil {
		t.Fatalf("SeedDefaults() error = %v", err)
	}
	content, err := service.Content(t.Context(), SystemPromptExecuteKey)
	if err != nil {
		t.Fatalf("Content(custom) error = %v", err)
	}
	if content != "用户已经改过" {
		t.Fatalf("custom content was overwritten: %q", content)
	}
	var count int64
	if err := db.Unscoped().Model(&domain.TextStorage{}).
		Where("storage_key = ?", SystemPromptResumeHumanKey).Count(&count).Error; err != nil {
		t.Fatalf("count deleted record: %v", err)
	}
	if count != 1 {
		t.Fatalf("deleted key row count = %d, want 1", count)
	}
	if _, err := service.Content(t.Context(), SystemPromptResumeHumanKey); err == nil {
		t.Fatal("soft-deleted built-in record must remain unavailable")
	}
}

func newTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE text_storage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		storage_key TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create text storage table: %v", err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service, db
}
