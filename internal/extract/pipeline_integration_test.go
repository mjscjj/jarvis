//go:build integration

package extract_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/ark"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/embedding"
	"jarvis/internal/extract"
	"jarvis/internal/extract/provider"
	"jarvis/internal/progress"
	"jarvis/internal/semantic"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/store"
	"jarvis/internal/textstore"
	"jarvis/internal/workrule"

	"github.com/qdrant/go-client/qdrant"
	"gorm.io/gorm"
)

// TestPipelineLive exercises SQLite -> facts -> model -> Todo persistence inside
// an outer transaction that is always rolled back.
func TestPipelineLive(t *testing.T) {
	configPath := os.Getenv("JARVIS_TEST_PIPELINE_CONFIG")
	if configPath == "" {
		t.Fatal("JARVIS_TEST_PIPELINE_CONFIG is required for live pipeline test")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("store.OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(db); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	if err := store.Migrate(db); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}

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
		SenderOpenID: cfg.Extract.PrincipalOpenID, SenderName: "测试用户", SenderType: "user",
		MessageType: "text", Content: "我明确承诺：在 jarvis 仓库修改鉴权逻辑。",
		CreateTime: now.UnixMilli(), Source: "poll", RenderOK: true,
	}
	if err := tx.Create(&message).Error; err != nil {
		t.Fatalf("create fixture message: %v", err)
	}

	modelClient, err := provider.NewClient(
		ark.BaseURL, ark.APIKey, cfg.Model.Model,
		time.Duration(cfg.Model.TimeoutSec)*time.Second,
	)
	if err != nil {
		t.Fatalf("provider.NewClient() error = %v", err)
	}
	location, err := time.LoadLocation(cfg.Capture.Timezone)
	if err != nil {
		t.Fatalf("time.LoadLocation() error = %v", err)
	}
	embeddingClient, err := embedding.NewClient(time.Duration(cfg.Model.TimeoutSec) * time.Second)
	if err != nil {
		t.Fatalf("embedding.NewClient() error = %v", err)
	}
	semanticCollection := fmt.Sprintf("todo_semantic_pipeline_test_%d", suffix)
	semanticIndex, err := semantic.NewIndex(semantic.Options{
		Host: cfg.Extract.QdrantHost, Port: cfg.Extract.QdrantGRPCPort, Collection: semanticCollection,
		EmbeddingModel: ark.EmbeddingModel, Dimensions: ark.EmbeddingDimensions, ScoreThreshold: cfg.Extract.SemanticThreshold,
		NeighborLimit: cfg.Extract.SemanticNeighborLimit, ActiveStatuses: extract.ActiveTodoStatuses(),
	})
	if err != nil {
		t.Fatalf("semantic.NewIndex() error = %v", err)
	}
	t.Cleanup(func() {
		if err := semanticIndex.Close(); err != nil {
			t.Errorf("semanticIndex.Close() error = %v", err)
		}
		cleanupClient, err := qdrant.NewClient(&qdrant.Config{
			Host: cfg.Extract.QdrantHost, Port: cfg.Extract.QdrantGRPCPort, PoolSize: 1, SkipCompatibilityCheck: true,
		})
		if err != nil {
			t.Errorf("create Qdrant cleanup client: %v", err)
			return
		}
		defer cleanupClient.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := cleanupClient.DeleteCollection(cleanupCtx, semanticCollection); err != nil {
			t.Errorf("delete semantic test collection: %v", err)
		}
	})
	ensureCtx, cancelEnsure := context.WithTimeout(context.Background(), 10*time.Second)
	if err := semanticIndex.Ensure(ensureCtx); err != nil {
		cancelEnsure()
		t.Fatalf("semanticIndex.Ensure() error = %v", err)
	}
	cancelEnsure()
	pipelineStore, err := extract.NewPipelineStore(tx, location, semanticIndex, "ou_test_principal")
	if err != nil {
		t.Fatalf("extract.NewPipelineStore() error = %v", err)
	}
	deduplicator, err := extract.NewDeduplicator(embeddingClient, semanticIndex, pipelineStore, modelClient)
	if err != nil {
		t.Fatalf("extract.NewDeduplicator() error = %v", err)
	}
	toolBoxBuilder, err := extract.NewRegistryToolBoxBuilder(tx, extract.ToolBoxConfig{
		ToolTimeout:     10 * time.Second,
		HistoryMaxLimit: 50,
		Location:        location,
	})
	if err != nil {
		t.Fatalf("extract.NewRegistryToolBoxBuilder() error = %v", err)
	}
	sharedMemoryService, err := sharedmem.NewSharedMemoryService(filepath.Join("..", "..", "data", "shared-memory.md"))
	if err != nil {
		t.Fatalf("sharedmem.NewSharedMemoryService() error = %v", err)
	}
	workRuleService, err := workrule.NewService(filepath.Join("..", "..", "conf", "rules"))
	if err != nil {
		t.Fatalf("workrule.NewService() error = %v", err)
	}
	skillService, err := skill.NewService(
		filepath.Join("..", "..", ".agents", "skills"),
		filepath.Join("..", "..", "conf", "skills.yaml"),
	)
	if err != nil {
		t.Fatalf("skill.NewService() error = %v", err)
	}
	textFileService, err := textstore.NewService(filepath.Join("..", "..", "conf", "prompts"))
	if err != nil {
		t.Fatalf("textstore.NewService() error = %v", err)
	}
	progressService, err := progress.NewService(tx)
	if err != nil {
		t.Fatalf("progress.NewService() error = %v", err)
	}
	worker, err := extract.NewWorker(pipelineStore, modelClient, progressService, deduplicator, toolBoxBuilder, sharedMemoryService, extract.WorkerOptions{
		Load: extract.LoadOptions{
			BatchMessages: 10, ContextMessages: cfg.Extract.ContextMessages,
			ContextWindow: time.Duration(cfg.Extract.ContextWindowMinutes) * time.Minute,
			OpenTodoLimit: cfg.Extract.OpenTodoLimit, RecentTaskLimit: cfg.Extract.RecentTaskLimit,
		},
		PrincipalOpenID: cfg.Extract.PrincipalOpenID, ModelName: cfg.Model.Model,
		FactLimit: cfg.Extract.FactLimit, KeyPersonLimit: cfg.Extract.KeyPersonLimit,
		MaxPromptChars: cfg.Extract.MaxPromptChars, Location: location,
		WorkRules:     workRuleService,
		Skills:        skillService,
		SystemPrompts: textFileService,
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

func TestPersistSemanticMatchLive(t *testing.T) {
	cfg, db := openPipelineTestDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	suffix := time.Now().UnixNano()
	chatID := fmt.Sprintf("oc_semantic_persist_%d", suffix)
	name := "semantic persistence fixture"
	group := domain.Group{ChatID: chatID, ChatMode: "group", Name: &name, Tier: "hot", RelatedGroup: true}
	if err := tx.Create(&group).Error; err != nil {
		t.Fatalf("create fixture group: %v", err)
	}
	sink := &recordingSemanticSink{}
	pipelineStore, err := extract.NewPipelineStore(tx, time.UTC, sink, "ou_test_principal")
	if err != nil {
		t.Fatalf("extract.NewPipelineStore() error = %v", err)
	}
	firstBatch, firstResult := semanticPersistFixture(group, "om_semantic_first", "修改鉴权", "修改旧鉴权逻辑")
	stats, err := pipelineStore.PersistChat(context.Background(), firstBatch, firstResult, cfg.Model.Model)
	if err != nil {
		t.Fatalf("first PersistChat() error = %v", err)
	}
	if stats.Created != 1 || len(sink.calls) != 1 || len(sink.calls[0]) != 1 {
		t.Fatalf("first stats=%#v sink.calls=%#v", stats, sink.calls)
	}
	var existing domain.Todo
	if err := tx.Where("group_id = ?", group.ID).Take(&existing).Error; err != nil {
		t.Fatalf("load created Todo: %v", err)
	}
	originalFingerprint := existing.DedupFingerprint

	secondBatch, secondResult := semanticPersistFixture(group, "om_semantic_second", "重构认证", "以新方案重构认证流程")
	secondResult[0].Candidates[0].Semantic.MatchedTodoID = &existing.ID
	stats, err = pipelineStore.PersistChat(context.Background(), secondBatch, secondResult, cfg.Model.Model)
	if err != nil {
		t.Fatalf("semantic PersistChat() error = %v", err)
	}
	if stats.Created != 0 || stats.Updated != 1 || len(sink.calls) != 2 {
		t.Fatalf("semantic stats=%#v sink.calls=%d", stats, len(sink.calls))
	}
	if err := tx.Where("id = ?", existing.ID).Take(&existing).Error; err != nil {
		t.Fatalf("reload semantic Todo: %v", err)
	}
	if existing.Title != "重构认证" || existing.DedupFingerprint != originalFingerprint || existing.Revision != 2 {
		t.Fatalf("updated Todo = %#v", existing)
	}
	lastRecord := sink.calls[1][0]
	if lastRecord.TodoID != existing.ID || lastRecord.Fingerprint != originalFingerprint || lastRecord.ActionType != "code_change" {
		t.Fatalf("semantic record = %#v", lastRecord)
	}
}

func TestPersistSemanticFailureRollsBackLive(t *testing.T) {
	cfg, db := openPipelineTestDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })

	suffix := time.Now().UnixNano()
	chatID := fmt.Sprintf("oc_semantic_failure_%d", suffix)
	name := "semantic rollback fixture"
	group := domain.Group{ChatID: chatID, ChatMode: "group", Name: &name, Tier: "hot", RelatedGroup: true}
	if err := tx.Create(&group).Error; err != nil {
		t.Fatalf("create fixture group: %v", err)
	}
	pipelineStore, err := extract.NewPipelineStore(tx, time.UTC, &recordingSemanticSink{err: errors.New("qdrant unavailable")}, "ou_test_principal")
	if err != nil {
		t.Fatalf("extract.NewPipelineStore() error = %v", err)
	}
	batch, results := semanticPersistFixture(group, "om_semantic_failure", "修改鉴权", "修改鉴权逻辑")
	if _, err := pipelineStore.PersistChat(context.Background(), batch, results, cfg.Model.Model); err == nil {
		t.Fatal("PersistChat() accepted semantic sync failure")
	}
	var todoCount, eventCount, watermarkCount int64
	if err := tx.Model(&domain.Todo{}).Where("group_id = ?", group.ID).Count(&todoCount).Error; err != nil {
		t.Fatalf("count Todos: %v", err)
	}
	if err := tx.Table("todo_event AS te").Joins("JOIN todo t ON t.id = te.todo_id").Where("t.group_id = ?", group.ID).Count(&eventCount).Error; err != nil {
		t.Fatalf("count Todo events: %v", err)
	}
	if err := tx.Model(&domain.TodoExtractWatermark{}).Where("chat_id = ?", chatID).Count(&watermarkCount).Error; err != nil {
		t.Fatalf("count watermarks: %v", err)
	}
	if todoCount != 0 || eventCount != 0 || watermarkCount != 0 {
		t.Fatalf("rollback counts: todo=%d event=%d watermark=%d", todoCount, eventCount, watermarkCount)
	}
}

type recordingSemanticSink struct {
	calls [][]semantic.Record
	err   error
}

func (s *recordingSemanticSink) Upsert(_ context.Context, records []semantic.Record) error {
	copyRecords := append([]semantic.Record(nil), records...)
	s.calls = append(s.calls, copyRecords)
	return s.err
}

func semanticPersistFixture(group domain.Group, messageID, title, summary string) (extract.ChatBatch, []extract.UnitExtraction) {
	message := extract.MessageContext{
		MessageID: messageID, ChatID: group.ChatID, SenderOpenID: "ou_owner", Content: title,
		CreateTime: time.Now().UnixMilli(), IsNew: true, Extractable: true,
	}
	candidate := extract.Candidate{
		ActionType: "code_change", Status: "extracted", Title: title, Target: title,
		Payload:          summary + "；repo jarvis。",
		SourceMessageIDs: []string{messageID}, SourceQuote: title,
	}
	batch := extract.ChatBatch{
		Group: extract.GroupContext{ID: group.ID, ChatID: group.ChatID},
		Units: []extract.ConversationUnit{{Key: "chat", Messages: []extract.MessageContext{message}}}, LastNew: message,
	}
	results := []extract.UnitExtraction{{UnitKey: "chat", Candidates: []extract.ResolvedCandidate{{
		Candidate: candidate, Semantic: extract.SemanticResolution{Vector: []float32{1}},
	}}}}
	return batch, results
}

func openPipelineTestDB(t *testing.T) (*config.Config, *gorm.DB) {
	t.Helper()
	configPath := os.Getenv("JARVIS_TEST_PIPELINE_CONFIG")
	if configPath == "" {
		t.Fatal("JARVIS_TEST_PIPELINE_CONFIG is required for live pipeline test")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("store.OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(db); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})
	if err := store.Migrate(db); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	return cfg, db
}
