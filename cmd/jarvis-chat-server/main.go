// Command jarvis-chat-server runs the right-side conversation independently
// from the Jarvis business server so rebuilding the latter cannot kill a turn.
package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"time"

	"jarvis/internal/api"
	"jarvis/internal/chat"
	"jarvis/internal/config"
	"jarvis/internal/contextsnap"
	"jarvis/internal/observability"
	"jarvis/internal/sharedmem"
	"jarvis/internal/store"
	"jarvis/internal/textstore"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

func main() {
	configPath := flag.String("config", "conf/config.yaml", "配置文件路径")
	flag.Parse()
	hlog.SetLevel(hlog.LevelInfo)
	startupCtx := observability.EnsureLogID(context.Background())
	fatalf := func(format string, args ...any) {
		hlog.CtxFatalf(startupCtx, format, args...)
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fatalf("load config failed: %v", err)
	}
	if !cfg.Chat.Enabled {
		fatalf("chat sidecar cannot start when chat.enabled=false")
	}
	absoluteConfig, err := filepath.Abs(*configPath)
	if err != nil {
		fatalf("resolve config path failed: %v", err)
	}
	repoRoot, err := os.Getwd()
	if err != nil {
		fatalf("resolve working directory failed: %v", err)
	}
	if err := cfg.ExportToolEnvironment(absoluteConfig, repoRoot); err != nil {
		fatalf("configure main service tool environment failed: %v", err)
	}
	textFileService, err := textstore.NewService(filepath.Join(filepath.Dir(absoluteConfig), "prompts"))
	if err != nil {
		fatalf("initialize chat system prompts failed: %v", err)
	}

	sharedMemoryPath, err := sharedmem.PathForConfig(absoluteConfig)
	if err != nil {
		fatalf("resolve shared memory path failed: %v", err)
	}
	sharedMemoryService, err := sharedmem.NewSharedMemoryService(sharedMemoryPath)
	if err != nil {
		fatalf("initialize shared memory service failed: %v", err)
	}
	connectCtx, cancel := context.WithTimeout(startupCtx, 10*time.Second)
	defer cancel()
	db, err := store.OpenReadOnlySQLite(connectCtx, cfg.SQLite)
	if err != nil {
		fatalf("open main database read-only failed: %v", err)
	}
	defer func() {
		if err := store.Close(db); err != nil {
			hlog.CtxErrorf(startupCtx, "close chat database failed: %v", err)
		}
	}()
	contextAssembler, err := contextsnap.NewAssembler(db, cfg.Extract.PrincipalOpenID)
	if err != nil {
		fatalf("initialize chat context assembler failed: %v", err)
	}
	chatService, err := chat.NewService(chat.Options{
		Bin:              cfg.Chat.Bin,
		Model:            cfg.Chat.Model,
		Sandbox:          cfg.Chat.Sandbox,
		ReasoningEffort:  cfg.Chat.ReasoningEffort,
		Timeout:          time.Duration(cfg.Chat.TimeoutSeconds) * time.Second,
		HistoryDir:       cfg.Chat.HistoryDir,
		SharedMemory:     sharedMemoryService,
		ContextAssembler: contextAssembler,
		SystemPrompts:    textFileService,
	})
	if err != nil {
		fatalf("initialize chat service failed: %v", err)
	}

	h := server.Default(
		server.WithHostPorts(cfg.Chat.Addr),
		server.WithMaxRequestBodySize(chat.MaxRequestBodyBytes),
	)
	if err := api.RegisterChatSidecar(h, chatService, db, cfg.Server.Addr); err != nil {
		fatalf("register chat sidecar routes failed: %v", err)
	}
	hlog.CtxInfof(startupCtx, "jarvis-chat-server listening on %s cli=%s model=%s", cfg.Chat.Addr, cfg.Chat.Bin, cfg.Chat.Model)
	h.Spin()
}
