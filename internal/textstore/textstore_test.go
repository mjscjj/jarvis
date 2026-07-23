package textstore

import (
	"fmt"
	"strings"
	"testing"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSystemPromptDefaultsDoNotEmbedToolManuals(t *testing.T) {
	for key, content := range map[string]string{
		SystemPromptM3Key: DefaultSystemPromptM3,
		SystemPromptM4Key: DefaultSystemPromptM4,
		SystemPromptM5Key: DefaultSystemPromptM5,
	} {
		for _, toolName := range []string{"jarvis-tools", "lark-cli", "bytedcli"} {
			if strings.Contains(content, toolName) {
				t.Fatalf("%s must not embed tool manual %q", key, toolName)
			}
		}
	}
}

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
		StorageKey: SystemPromptM5Key,
		Name:       "自定义 M5 系统提示词",
		Content:    "用户已经改过",
	}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatalf("create custom record: %v", err)
	}
	deleted := domain.TextStorage{
		StorageKey: SystemPromptM4Key,
		Name:       "已删除的 M4 系统提示词",
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
	content, err := service.Content(t.Context(), SystemPromptM5Key)
	if err != nil {
		t.Fatalf("Content(custom) error = %v", err)
	}
	if content != "用户已经改过" {
		t.Fatalf("custom content was overwritten: %q", content)
	}
	var count int64
	if err := db.Unscoped().Model(&domain.TextStorage{}).
		Where("storage_key = ?", SystemPromptM4Key).Count(&count).Error; err != nil {
		t.Fatalf("count deleted record: %v", err)
	}
	if count != 1 {
		t.Fatalf("deleted key row count = %d, want 1", count)
	}
	if _, err := service.Content(t.Context(), SystemPromptM4Key); err == nil {
		t.Fatal("soft-deleted built-in record must remain unavailable")
	}
}

func TestSeedDefaultsRetiresLegacySystemPrompts(t *testing.T) {
	service, db := newTestService(t)
	legacy := domain.TextStorage{StorageKey: legacySystemPromptKeys[0], Name: "旧 M5", Content: "旧内容"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("create legacy prompt: %v", err)
	}
	if err := service.SeedDefaults(t.Context()); err != nil {
		t.Fatalf("SeedDefaults() error = %v", err)
	}
	if _, err := service.Content(t.Context(), legacy.StorageKey); err == nil {
		t.Fatal("legacy system prompt must be retired")
	}
	var count int64
	if err := db.Unscoped().Model(&domain.TextStorage{}).Where("storage_key = ?", legacy.StorageKey).Count(&count).Error; err != nil {
		t.Fatalf("count retired legacy prompt: %v", err)
	}
	if count != 1 {
		t.Fatalf("retired legacy row count = %d, want 1", count)
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
