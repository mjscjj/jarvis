package okrchat

import (
	"context"
	"path/filepath"
	"time"

	"jarvis/internal/chat"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/okrworkspace/moduleconfig"
	"jarvis/internal/store"
	"jarvis/internal/textstore"
)

// openLocal uses the ordinary Chat runner and this instance's credentials.
// The caller chooses this mode only in an instance whose entire data/API may
// be used by its OKR agents. OwnerRequired still isolates browser histories.
func openLocal(ctx context.Context, cfg moduleconfig.ChatConfig, root, name string, prompts textstore.Reader) (*chat.Service, func(), error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	db, err := store.OpenSQLite(ctx, config.SQLiteConfig{Path: filepath.Join(root, "chat.db")})
	if err != nil {
		return nil, nil, err
	}
	closeDB := func() { _ = store.Close(db) }
	if err := db.AutoMigrate(domain.ChatModels()...); err != nil {
		closeDB()
		return nil, nil, err
	}
	service, err := chat.NewService(chat.Options{
		AgentName: name, Bin: "codex", Model: cfg.Model,
		Sandbox: "danger-full-access", ReasoningEffort: cfg.ReasoningEffort,
		Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		DB:      db, FilesRoot: filepath.Join(root, "files"), Prompts: prompts,
		OwnerRequired: true, PromptKey: textstore.SystemPromptOKRChatKey,
	})
	if err != nil {
		closeDB()
		return nil, nil, err
	}
	return service, closeDB, nil
}
