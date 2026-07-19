package extract_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/extract"
	"jarvis/internal/extract/provider"
	"jarvis/internal/memory"
	"jarvis/internal/store"

	"gorm.io/gorm"
)

// TestPipelineLive exercises MySQL -> mem0 -> model -> Todo persistence inside
// an outer transaction that is always rolled back. Existing related groups are
// hidden only inside that transaction, so no real Feishu message is sent to the
// model and no fixture remains in the production database.
func TestPipelineLive(t *testing.T) {
	configPath := os.Getenv("JARVIS_TEST_PIPELINE_CONFIG")
	if configPath == "" {
		t.Skip("JARVIS_TEST_PIPELINE_CONFIG is required for live pipeline test")
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

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	rolledBack := false
	t.Cleanup(func() {
		if !rolledBack {
			_ = tx.Rollback().Error
		}
	})
	if err := tx.Model(&domain.Group{}).Where("related_group = ?", true).Update("related_group", false).Error; err != nil {
		t.Fatalf("isolate existing related groups: %v", err)
	}

	suffix := time.Now().UnixNano()
	chatID := fmt.Sprintf("oc_pipeline_fixture_%d", suffix)
	messageID := fmt.Sprintf("om_pipeline_fixture_%d", suffix)
	groupName := "Jarvis pipeline fixture"
	group := domain.Group{
		ChatID: chatID, ChatMode: "group", Name: &groupName, RelatedGroup: true,
		Tier: "hot", IncludeInMemory: true, IsKeyGroup: true,
	}
	if err := tx.Create(&group).Error; err != nil {
		t.Fatalf("create fixture group: %v", err)
	}
	now := time.Now()
	message := domain.Message{
		MessageID: messageID, ChatID: chatID, GroupID: &group.ID, ChatMode: "group",
		SenderOpenID: cfg.Extract.PrincipalOpenID, SenderName: "储节节", SenderType: "user",
		MessageType: "text", Content: "我明确承诺：在 jarvis 仓库修改鉴权逻辑。",
		CreateTime: now.UnixMilli(), Source: "poll", RenderOK: true,
	}
	if err := tx.Create(&message).Error; err != nil {
		t.Fatalf("create fixture message: %v", err)
	}

	modelClient, err := provider.NewClient(
		cfg.Model.BaseURL, cfg.Model.APIKey, cfg.Model.Model,
		time.Duration(cfg.Model.TimeoutSec)*time.Second,
	)
	if err != nil {
		t.Fatalf("provider.NewClient() error = %v", err)
	}
	memoryClient, err := memory.NewClient(cfg.Mem0.BaseURL, time.Duration(cfg.Mem0.TimeoutSec)*time.Second)
	if err != nil {
		t.Fatalf("memory.NewClient() error = %v", err)
	}
	location, err := time.LoadLocation(cfg.Capture.Timezone)
	if err != nil {
		t.Fatalf("time.LoadLocation() error = %v", err)
	}
	pipelineStore, err := extract.NewPipelineStore(tx, location)
	if err != nil {
		t.Fatalf("extract.NewPipelineStore() error = %v", err)
	}
	worker, err := extract.NewWorker(pipelineStore, modelClient, memoryClient, extract.WorkerOptions{
		Load: extract.LoadOptions{
			BatchMessages: 10, ContextMessages: cfg.Extract.ContextMessages,
			ContextWindow: time.Duration(cfg.Extract.ContextWindowMinutes) * time.Minute,
			OpenTodoLimit: cfg.Extract.OpenTodoLimit,
		},
		PrincipalOpenID: cfg.Extract.PrincipalOpenID, ModelName: cfg.Model.Model,
		MemoryTopK: cfg.Extract.MemoryTopK, MemoryThreshold: cfg.Extract.MemoryThreshold,
		MaxPromptChars: cfg.Extract.MaxPromptChars, Location: location,
	})
	if err != nil {
		t.Fatalf("extract.NewWorker() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stats, err := worker.ExtractOnce(ctx)
	if err != nil {
		t.Fatalf("ExtractOnce() error = %v", err)
	}
	if stats.ChatsProcessed != 1 || stats.Units != 1 || stats.Created < 1 {
		t.Fatalf("stats = %#v", stats)
	}
	var count int64
	if err := tx.Model(&domain.Todo{}).Where("group_id = ?", group.ID).Count(&count).Error; err != nil {
		t.Fatalf("count fixture todos: %v", err)
	}
	if count < 1 {
		t.Fatalf("fixture todo count = %d, want at least 1", count)
	}
	if err := tx.Rollback().Error; err != nil && err != gorm.ErrInvalidTransaction {
		t.Fatalf("rollback fixture transaction: %v", err)
	}
	rolledBack = true
	if err := db.Model(&domain.Group{}).Where("chat_id = ?", chatID).Count(&count).Error; err != nil {
		t.Fatalf("verify fixture rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("fixture group remains after rollback: count=%d", count)
	}
}
