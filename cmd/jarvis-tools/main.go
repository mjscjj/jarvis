// Command jarvis-tools is the decision-query tool set that codex self-runs
// during M3 extraction / M4 decision / M5 execution (docs/design-context-pipeline.md
// §2.1a). It exposes Jarvis's own MySQL/mem0 data — the part codex cannot reach
// via lark-cli/bytedcli/git — as small subcommands.
//
// Most subcommands are read-only. The exception is the shared-memory writers
// (set-shared-memory / append-shared-memory): a small set of controlled writes
// that let the agent persist "共享记忆"（踩过的坑/关键约定/凭据）for later runs.
// All other subcommands stay read-only.
//
// Output contract (strict): each subcommand prints compact JSON to stdout and
// NOTHING else, so codex can parse it reliably. Any error is written to stderr
// and the process exits non-zero (fail-fast).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/store"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// sharedMemoryUpdatedBy 标记共享记忆写入来源为 agent（区别于人工在后台的编辑）。
const sharedMemoryUpdatedBy = "agent"

const connectTimeout = 10 * time.Second

func main() {
	if len(os.Args) < 2 {
		fail(fmt.Errorf("usage: jarvis-tools <subcommand> [flags]\nsubcommands: list-projects get-project get-group get-principal get-person get-shared-memory get-skill set-shared-memory append-shared-memory"))
	}
	subcommand := os.Args[1]
	args := os.Args[2:]

	if err := run(subcommand, args); err != nil {
		fail(err)
	}
}

func run(subcommand string, args []string) error {
	switch subcommand {
	case "list-projects":
		return runListProjects(args)
	case "get-project":
		return runGetProject(args)
	case "get-group":
		return runGetGroup(args)
	case "get-principal":
		return runGetPrincipal(args)
	case "get-person":
		return runGetPerson(args)
	case "get-shared-memory":
		return runGetSharedMemory(args)
	case "get-skill":
		return runGetSkill(args)
	case "set-shared-memory":
		return runSetSharedMemory(args)
	case "append-shared-memory":
		return runAppendSharedMemory(args)
	default:
		return fmt.Errorf("unknown subcommand %q; want one of: list-projects get-project get-group get-principal get-person get-shared-memory get-skill set-shared-memory append-shared-memory", subcommand)
	}
}

// openDB loads config and connects to MySQL. It never runs migrations — schema
// is owned by the jarvis-server main process; the tool only reads, and (for the
// shared-memory writers) writes rows into already-migrated tables.
func openDB(configPath string) (*config.Config, *gorm.DB, func(), error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	db, err := store.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		return nil, nil, nil, err
	}
	// Keep stderr clean: the JSON-only stdout contract means the only thing on
	// stderr should be our own fatal error line. Silence GORM's own logger so a
	// legitimate "record not found" does not leak SQL warnings to codex.
	db.Logger = gormlogger.Default.LogMode(gormlogger.Silent)
	cleanup := func() { _ = store.Close(db) }
	return cfg, db, cleanup, nil
}

func runListProjects(args []string) error {
	fs := flag.NewFlagSet("list-projects", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := background.NewProjectService(db)
	if err != nil {
		return err
	}
	projects, err := svc.ListAll(context.Background())
	if err != nil {
		return err
	}
	return emit(map[string]any{"projects": projects})
}

func runGetProject(args []string) error {
	fs := flag.NewFlagSet("get-project", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	id := fs.Uint64("id", 0, "project id")
	code := fs.String("code", "", "project code")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*id == 0) == (*code == "") {
		return fmt.Errorf("get-project requires exactly one of --id or --code")
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := background.NewProjectService(db)
	if err != nil {
		return err
	}
	var project any
	if *id != 0 {
		project, err = svc.Get(context.Background(), *id)
	} else {
		project, err = svc.GetByCode(context.Background(), *code)
	}
	if err != nil {
		return mapNotFound(err)
	}
	return emit(project)
}

func runGetGroup(args []string) error {
	fs := flag.NewFlagSet("get-group", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	chatID := fs.String("chat-id", "", "feishu chat_id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *chatID == "" {
		return fmt.Errorf("get-group requires --chat-id")
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := background.NewGroupBackgroundService(db, nil)
	if err != nil {
		return err
	}
	group, err := svc.GetByChatID(context.Background(), *chatID)
	if err != nil {
		return mapNotFound(err)
	}
	return emit(group)
}

func runGetPrincipal(args []string) error {
	fs := flag.NewFlagSet("get-principal", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := background.NewProfileService(db, cfg.Extract.PrincipalOpenID)
	if err != nil {
		return err
	}
	profile, err := svc.Get(context.Background())
	if err != nil {
		return err
	}
	return emit(profile)
}

func runGetPerson(args []string) error {
	fs := flag.NewFlagSet("get-person", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	openID := fs.String("open-id", "", "feishu open_id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *openID == "" {
		return fmt.Errorf("get-person requires --open-id")
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := background.NewPersonService(db)
	if err != nil {
		return err
	}
	person, err := svc.GetByOpenID(context.Background(), *openID)
	if err != nil {
		return mapNotFound(err)
	}
	return emit(person)
}

func runGetSharedMemory(args []string) error {
	fs := flag.NewFlagSet("get-shared-memory", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := sharedmem.NewSharedMemoryService(db)
	if err != nil {
		return err
	}
	view, err := svc.Get(context.Background())
	if err != nil {
		return err
	}
	return emit(view)
}

func runGetSkill(args []string) error {
	fs := flag.NewFlagSet("get-skill", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	name := fs.String("name", "", "skill name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" {
		return fmt.Errorf("get-skill requires --name")
	}
	cfg, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := skill.NewService(db, cfg.Skills.Root)
	if err != nil {
		return err
	}
	view, err := svc.Content(context.Background(), *name)
	if err != nil {
		return mapNotFound(err)
	}
	return emit(view)
}

func runSetSharedMemory(args []string) error {
	fs := flag.NewFlagSet("set-shared-memory", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	content := fs.String("content", "-", `full text to overwrite shared memory with; "-" (default) reads from stdin`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	text, err := readContentArg(*content)
	if err != nil {
		return err
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := sharedmem.NewSharedMemoryService(db)
	if err != nil {
		return err
	}
	view, err := svc.Upsert(context.Background(), text, sharedMemoryUpdatedBy)
	if err != nil {
		return err
	}
	return emit(view)
}

func runAppendSharedMemory(args []string) error {
	fs := flag.NewFlagSet("append-shared-memory", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	note := fs.String("note", "-", `one entry to append to shared memory; "-" (default) reads from stdin`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	text, err := readContentArg(*note)
	if err != nil {
		return err
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	svc, err := sharedmem.NewSharedMemoryService(db)
	if err != nil {
		return err
	}
	view, err := svc.Append(context.Background(), text, sharedMemoryUpdatedBy)
	if err != nil {
		return err
	}
	return emit(view)
}

// readContentArg resolves a text flag value: the literal "-" (or empty) means
// read the whole payload from stdin, letting codex pipe long text without hitting
// command-line length limits; any other value is used verbatim.
func readContentArg(value string) (string, error) {
	if value != "" && value != "-" {
		return value, nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read content from stdin: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// emit writes the value as compact JSON to stdout followed by a newline. This is
// the ONLY thing a successful subcommand writes to stdout.
func emit(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode result JSON: %w", err)
	}
	if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	return nil
}

// mapNotFound turns background.ErrNotFound into a clear message so codex sees a
// deterministic "not found" instead of an opaque error.
func mapNotFound(err error) error {
	if errors.Is(err, background.ErrNotFound) || errors.Is(err, skill.ErrNotFound) {
		return fmt.Errorf("not found")
	}
	return err
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "jarvis-tools error:", err.Error())
	os.Exit(1)
}
