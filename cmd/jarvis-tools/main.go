// Command jarvis-tools is the decision-query tool set that codex self-runs
// during M3 extraction / M4 decision / M5 execution (docs/design-context-pipeline.md
// §2.1a). It exposes Jarvis's own MySQL/mem0 data — the part codex cannot reach
// via lark-cli/bytedcli/git — as small subcommands.
//
// Most subcommands are read-only. Controlled writes cover shared memory and
// recurring scheduled tasks, both explicitly exposed for agent use.
//
// Output contract (strict): data subcommands print compact JSON to stdout and
// NOTHING else, so codex can parse it reliably. Help is plain text; any error is
// written to stderr and the process exits non-zero (fail-fast).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/knowledge"
	"jarvis/internal/progress"
	"jarvis/internal/scheduledtask"
	"jarvis/internal/sharedmem"
	"jarvis/internal/skill"
	"jarvis/internal/store"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const connectTimeout = 10 * time.Second

func main() {
	if len(os.Args) < 2 {
		fail(fmt.Errorf("usage: jarvis-tools <subcommand> [flags]\nsubcommands: list-projects get-project get-group get-principal get-person get-shared-memory get-skill set-shared-memory append-shared-memory list-scheduled-tasks create-scheduled-task yield-until delete-scheduled-task"))
	}
	subcommand := os.Args[1]
	args := os.Args[2:]
	if subcommand == "-h" || subcommand == "--help" {
		fmt.Println("usage: jarvis-tools <subcommand> [flags]")
		fmt.Println("subcommands: list-projects get-project get-group get-principal get-person get-shared-memory get-skill set-shared-memory append-shared-memory list-scheduled-tasks create-scheduled-task yield-until delete-scheduled-task")
		return
	}

	if err := run(subcommand, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
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
	case "list-scheduled-tasks":
		return runListScheduledTasks(args)
	case "create-scheduled-task":
		return runCreateScheduledTask(args)
	case "yield-until":
		return runYieldUntil(args)
	case "delete-scheduled-task":
		return runDeleteScheduledTask(args)
	default:
		return fmt.Errorf("unknown subcommand %q; want one of: list-projects get-project get-group get-principal get-person get-shared-memory get-skill set-shared-memory append-shared-memory list-scheduled-tasks create-scheduled-task yield-until delete-scheduled-task", subcommand)
	}
}

// openDB loads config and connects to MySQL. It never runs migrations — schema
// is owned by the jarvis-server main process.
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
	var project *background.ProjectView
	if *id != 0 {
		project, err = svc.Get(context.Background(), *id)
	} else {
		project, err = svc.GetByCode(context.Background(), *code)
	}
	if err != nil {
		return mapNotFound(err)
	}
	eventService, err := progress.NewService(db)
	if err != nil {
		return err
	}
	events, err := eventService.ListProjectEvents(context.Background(), project.ID)
	if err != nil {
		return err
	}
	if len(events) > 50 {
		events = events[:50]
	}
	factService, err := knowledge.NewService(db)
	if err != nil {
		return err
	}
	entityType := knowledge.EntityProject
	entityID := project.ID
	relations, err := factService.List(context.Background(), knowledge.FactFilter{
		EntityType: &entityType, EntityID: &entityID, Page: 1, PageSize: 100,
	})
	if err != nil {
		return err
	}
	return emit(map[string]any{
		"project": project, "project_events": events, "relation_facts": relations.Items,
	})
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
	factService, err := knowledge.NewService(db)
	if err != nil {
		return err
	}
	entityType := knowledge.EntityPerson
	entityID := person.ID
	relations, err := factService.List(context.Background(), knowledge.FactFilter{
		EntityType: &entityType, EntityID: &entityID, Page: 1, PageSize: 100,
	})
	if err != nil {
		return err
	}
	return emit(map[string]any{"person": person, "relation_facts": relations.Items})
}

func runGetSharedMemory(args []string) error {
	fs := flag.NewFlagSet("get-shared-memory", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := sharedmem.PathForConfig(*configPath)
	if err != nil {
		return err
	}
	svc, err := sharedmem.NewSharedMemoryService(path)
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
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	svc, err := skill.NewService(cfg.Skills.Root, filepath.Join(filepath.Dir(*configPath), "skills.yaml"))
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
	path, err := sharedmem.PathForConfig(*configPath)
	if err != nil {
		return err
	}
	svc, err := sharedmem.NewSharedMemoryService(path)
	if err != nil {
		return err
	}
	view, err := svc.Upsert(context.Background(), text)
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
	path, err := sharedmem.PathForConfig(*configPath)
	if err != nil {
		return err
	}
	svc, err := sharedmem.NewSharedMemoryService(path)
	if err != nil {
		return err
	}
	view, err := svc.Append(context.Background(), text)
	if err != nil {
		return err
	}
	return emit(view)
}

func runListScheduledTasks(args []string) error {
	fs := flag.NewFlagSet("list-scheduled-tasks", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	status := fs.String("status", "", "optional status: active/running/completed")
	limit := fs.Int("limit", 200, "maximum rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	service, err := scheduledtask.NewCRUDService(db)
	if err != nil {
		return err
	}
	items, err := service.List(context.Background(), scheduledtask.ListFilter{Status: *status, Limit: *limit})
	if err != nil {
		return err
	}
	return emit(map[string]any{"items": items})
}

func runCreateScheduledTask(args []string) error {
	fs := flag.NewFlagSet("create-scheduled-task", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	payload := fs.String("payload", "-", `JSON object; "-" (default) reads stdin`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	text, err := readContentArg(*payload)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	var input scheduledtask.Input
	if err := decoder.Decode(&input); err != nil {
		return fmt.Errorf("decode scheduled task payload: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	service, err := scheduledtask.NewCRUDService(db)
	if err != nil {
		return err
	}
	view, err := service.Create(context.Background(), input)
	if err != nil {
		return err
	}
	return emit(view)
}

func runYieldUntil(args []string) error {
	fs := flag.NewFlagSet("yield-until", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	at := fs.String("at", "", "RFC3339 wake time")
	reasonArg := fs.String("reason", "-", `reason text; "-" reads stdin`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	taskID, err := strconv.ParseUint(strings.TrimSpace(os.Getenv("JARVIS_TASK_ID")), 10, 64)
	if err != nil || taskID == 0 {
		return fmt.Errorf("yield-until requires JARVIS_TASK_ID from the Task runner")
	}
	runAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*at))
	if err != nil {
		return fmt.Errorf("yield-until --at must be RFC3339: %w", err)
	}
	reason, err := readContentArg(*reasonArg)
	if err != nil {
		return err
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	service, err := scheduledtask.NewCRUDService(db)
	if err != nil {
		return err
	}
	view, err := service.CreateYield(context.Background(), scheduledtask.YieldInput{
		TaskID: taskID, RunAt: runAt, Reason: reason,
	})
	if err != nil {
		return err
	}
	return emit(map[string]any{
		"scheduled_task_id": view.ID, "wake_at": view.NextRunAt, "reason": strings.TrimSpace(reason),
	})
}

func runDeleteScheduledTask(args []string) error {
	fs := flag.NewFlagSet("delete-scheduled-task", flag.ContinueOnError)
	configPath := fs.String("config", "conf/config.yaml", "config file path")
	id := fs.Uint64("id", 0, "scheduled task id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == 0 {
		return fmt.Errorf("delete-scheduled-task requires --id")
	}
	_, db, cleanup, err := openDB(*configPath)
	if err != nil {
		return err
	}
	defer cleanup()
	service, err := scheduledtask.NewCRUDService(db)
	if err != nil {
		return err
	}
	if err := service.Delete(context.Background(), *id); err != nil {
		return mapNotFound(err)
	}
	return emit(map[string]any{"id": *id, "deleted": true})
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return fmt.Errorf("scheduled task payload must contain exactly one JSON object")
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
	if errors.Is(err, background.ErrNotFound) || errors.Is(err, skill.ErrNotFound) || errors.Is(err, scheduledtask.ErrNotFound) {
		return fmt.Errorf("not found")
	}
	return err
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "jarvis-tools error:", err.Error())
	os.Exit(1)
}
