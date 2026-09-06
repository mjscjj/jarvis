package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/scheduledtask"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeRunner struct {
	run func(string, []string) ([]byte, error)
}

func (f fakeRunner) Run(_ context.Context, bin string, args ...string) ([]byte, error) {
	return f.run(bin, args)
}

type fakeScheduler struct {
	items    map[uint64]scheduledtask.View
	nextID   uint64
	triggers int
}

func newFakeScheduler() *fakeScheduler {
	return &fakeScheduler{items: make(map[uint64]scheduledtask.View), nextID: 1}
}

func (f *fakeScheduler) Create(_ context.Context, input scheduledtask.Input) (*scheduledtask.View, error) {
	id := f.nextID
	f.nextID++
	view := scheduledtask.View{
		ID: id, Title: input.Title, ActionType: input.ActionType, Instruction: input.Instruction,
		DispatchKind: input.DispatchKind, DispatchPayload: input.DispatchPayload,
		ContextSnapshot: input.ContextSnapshot, ScheduleType: input.ScheduleType,
		IntervalMinutes: input.IntervalMinutes, Enabled: *input.Enabled, Status: "active",
		NextRunAt: time.Now().Add(time.Hour),
	}
	f.items[id] = view
	return cloneSchedule(view), nil
}

func (f *fakeScheduler) Get(_ context.Context, id uint64) (*scheduledtask.View, error) {
	view, ok := f.items[id]
	if !ok {
		return nil, scheduledtask.ErrNotFound
	}
	return cloneSchedule(view), nil
}

func (f *fakeScheduler) Update(_ context.Context, id uint64, input scheduledtask.Input) (*scheduledtask.View, error) {
	view, ok := f.items[id]
	if !ok {
		return nil, scheduledtask.ErrNotFound
	}
	view.Enabled = *input.Enabled
	view.Title = input.Title
	view.Instruction = input.Instruction
	f.items[id] = view
	return cloneSchedule(view), nil
}

func (f *fakeScheduler) Trigger(_ context.Context, id uint64) (*scheduledtask.View, error) {
	view, ok := f.items[id]
	if !ok {
		return nil, scheduledtask.ErrNotFound
	}
	f.triggers++
	status := "done"
	view.LastRunStatus = &status
	now := time.Now()
	view.LastFinishedAt = &now
	f.items[id] = view
	return cloneSchedule(view), nil
}

func cloneSchedule(view scheduledtask.View) *scheduledtask.View {
	copy := view
	return &copy
}

func openPluginDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.PluginInstallation{}, &domain.Message{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func onePluginRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewRegistry([]Manifest{{
		ID: "codebase", Name: "Codebase", Description: "MR",
		Source: "codebase", CollectorSkill: "codebase-clue-collector",
		Provider: "bytedcli-session", IntervalMinutes: 30,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestServiceDefaultsDisabledAndGatesOwnedSkill(t *testing.T) {
	db := openPluginDB(t)
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return []byte(`{"status":"success","data":{"authenticated":true}}`), nil
	}})
	service, err := NewService(db, onePluginRegistry(t), authorizer, newFakeScheduler())
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Enabled || items[0].State != "disabled" {
		t.Fatalf("items = %#v", items)
	}
	enabled, err := service.SkillEnabled(t.Context(), "codebase-clue-collector")
	if err != nil || enabled {
		t.Fatalf("SkillEnabled() = %v, %v", enabled, err)
	}
	unowned, err := service.SkillEnabled(t.Context(), "feishu-send-message")
	if err != nil || !unowned {
		t.Fatalf("unowned SkillEnabled() = %v, %v", unowned, err)
	}
}

func TestEnableAuthorizedPluginCreatesAndTriggersSchedule(t *testing.T) {
	db := openPluginDB(t)
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return []byte(`{"status":"success","data":{"authenticated":true}}`), nil
	}})
	scheduler := newFakeScheduler()
	service, err := NewService(db, onePluginRegistry(t), authorizer, scheduler)
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Update(t.Context(), "codebase", UpdateInput{Enabled: true, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !view.Enabled || view.State != "ready" || view.ScheduledTaskID == nil {
		t.Fatalf("view = %#v", view)
	}
	if scheduler.triggers != 1 {
		t.Fatalf("triggers = %d, want 1", scheduler.triggers)
	}
	if !strings.Contains(scheduler.items[*view.ScheduledTaskID].Instruction, "codebase-clue-collector") {
		t.Fatalf("schedule instruction = %q", scheduler.items[*view.ScheduledTaskID].Instruction)
	}
	if _, err := service.Update(t.Context(), "codebase", UpdateInput{Enabled: false, ExpectedRevision: view.Revision}); err != nil {
		t.Fatal(err)
	}
	if scheduler.items[*view.ScheduledTaskID].Enabled {
		t.Fatal("disabled plugin left its schedule enabled")
	}
}

func TestUpdateConfigPersistsAndRefreshesScheduleInstruction(t *testing.T) {
	db := openPluginDB(t)
	registry, err := NewRegistry([]Manifest{{
		ID: "oncall", Name: "Oncall", Description: "groups",
		Source: "oncall", CollectorSkill: "oncall-clue-collector",
		Provider: "lark-cli-im", IntervalMinutes: 15,
		DefaultConfig: json.RawMessage(`{"search_terms":["oncall","值班"]}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return []byte(`{"ok":true,"data":{"chats":null,"total":0}}`), nil
	}})
	scheduler := newFakeScheduler()
	service, err := NewService(db, registry, authorizer, scheduler)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.Get(t.Context(), "oncall")
	if err != nil {
		t.Fatal(err)
	}
	if string(initial.Config) != `{"search_terms":["oncall","值班"]}` {
		t.Fatalf("default config = %s", initial.Config)
	}
	config := json.RawMessage(`{"search_terms":["SRE 告警","线上事故"]}`)
	updated, err := service.Update(t.Context(), "oncall", UpdateInput{
		Enabled: true, ExpectedRevision: initial.Revision, Config: &config,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(updated.Config) != string(config) {
		t.Fatalf("updated config = %s", updated.Config)
	}
	instruction := scheduler.items[*updated.ScheduledTaskID].Instruction
	if !strings.Contains(instruction, `"SRE 告警"`) || !strings.Contains(instruction, `"线上事故"`) {
		t.Fatalf("schedule instruction = %q", instruction)
	}
}

func TestBuiltinMeegoDefaultsToThirtyDayLookback(t *testing.T) {
	registry, err := BuiltinRegistry()
	if err != nil {
		t.Fatal(err)
	}
	manifest, ok := registry.Get("meego")
	if !ok {
		t.Fatal("Meego manifest is missing")
	}
	if string(manifest.DefaultConfig) != `{"lookback_days":30}` {
		t.Fatalf("Meego default config = %s", manifest.DefaultConfig)
	}
	instruction := scheduleInput(manifest, manifest.DefaultConfig, true).Instruction
	if !strings.Contains(instruction, `"lookback_days":30`) {
		t.Fatalf("Meego schedule instruction = %q", instruction)
	}
}

func TestUpdateRejectsNonObjectConfig(t *testing.T) {
	db := openPluginDB(t)
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return []byte(`{"status":"success","data":{"authenticated":true}}`), nil
	}})
	service, err := NewService(db, onePluginRegistry(t), authorizer, newFakeScheduler())
	if err != nil {
		t.Fatal(err)
	}
	config := json.RawMessage(`["not","an","object"]`)
	if _, err := service.Update(t.Context(), "codebase", UpdateInput{
		ExpectedRevision: 0, Config: &config,
	}); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("Update() error = %v", err)
	}
}

func TestTaskExecutionError(t *testing.T) {
	got := taskExecutionError([]byte(`{"stage":"executed","error":"Your requests have exceeded the quota."}`))
	if got != "Your requests have exceeded the quota." {
		t.Fatalf("taskExecutionError() = %q", got)
	}
}

func TestEnableUnauthorizedPluginWaitsWithoutSchedule(t *testing.T) {
	db := openPluginDB(t)
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return []byte(`{"status":"error","error":{"message":"authentication required"}}`), errors.New("exit 1")
	}})
	service, err := NewService(db, onePluginRegistry(t), authorizer, newFakeScheduler())
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Update(t.Context(), "codebase", UpdateInput{Enabled: true, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "needs_auth" || view.ScheduledTaskID != nil {
		t.Fatalf("view = %#v", view)
	}
}

func TestUpdateRejectsStaleRevision(t *testing.T) {
	db := openPluginDB(t)
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, _ []string) ([]byte, error) {
		return []byte(`{"status":"error","error":{"message":"authentication required"}}`), errors.New("exit 1")
	}})
	service, err := NewService(db, onePluginRegistry(t), authorizer, newFakeScheduler())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(t.Context(), "codebase", UpdateInput{Enabled: true, ExpectedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(t.Context(), "codebase", UpdateInput{Enabled: false, ExpectedRevision: 0}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Update() error = %v, want ErrConflict", err)
	}
}

func TestAuthorizerBeginAndComplete(t *testing.T) {
	calls := 0
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		calls++
		command := strings.Join(args, " ")
		switch {
		case strings.Contains(command, "auth status"):
			return []byte(`{"status":"success","data":{"authenticated":false}}`), nil
		case strings.Contains(command, "--begin"):
			return []byte("{\"event\":\"qr_image_ready\",\"data\":{\"complete_token\":\"resume-1\",\"verification_uri_complete\":\"https://example.test/login\",\"user_code\":\"ABCD\"}}\n"), nil
		case strings.Contains(command, "--complete"):
			return []byte(`{"status":"success","data":{"authenticated":true}}`), nil
		default:
			return nil, fmt.Errorf("unexpected args: %s", command)
		}
	}})
	begin := authorizer.Begin(t.Context(), "bytedcli-session")
	if begin.Status != AuthPending || begin.FlowID == nil || begin.VerificationURL == nil {
		t.Fatalf("begin = %#v", begin)
	}
	complete := authorizer.Complete(t.Context(), "bytedcli-session", *begin.FlowID)
	if complete.Status != AuthAuthorized {
		t.Fatalf("complete = %#v", complete)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestAuthorizerRejectsFlowForDifferentProvider(t *testing.T) {
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.Contains(command, "auth status"):
			return []byte(`{"status":"success","data":{"authenticated":false}}`), nil
		case strings.Contains(command, "--begin"):
			return []byte(`{"data":{"complete_token":"resume-1","verification_uri_complete":"https://example.test/login"}}`), nil
		default:
			return nil, fmt.Errorf("unexpected args: %s", command)
		}
	}})
	begin := authorizer.Begin(t.Context(), "bytedcli-session")
	if begin.FlowID == nil {
		t.Fatalf("begin = %#v", begin)
	}
	complete := authorizer.Complete(t.Context(), "lark-cli-im", *begin.FlowID)
	if complete.Status != AuthFailed {
		t.Fatalf("complete = %#v", complete)
	}
}

func TestProbeReadsStructuredAuthorizationState(t *testing.T) {
	authorizer := newAuthorizer(fakeRunner{run: func(_ string, args []string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "--json meego status":
			return []byte(`{"status":"success","data":{"authenticated":false}}`), nil
		case "im +chat-search --as user --query oncall --disable-search-by-user --chat-modes group,topic --search-types private,public_joined --page-size 1 --page-limit 1 --format json":
			return []byte(`{"ok":true,"identity":"user","data":{"chats":null,"has_more":false,"page_token":"","total":0}}`), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}})
	if status := authorizer.Probe(t.Context(), "meego"); status.Status != AuthRequired {
		t.Fatalf("meego Probe() = %#v", status)
	}
	if status := authorizer.Probe(t.Context(), "lark-cli-im"); status.Status != AuthAuthorized {
		t.Fatalf("oncall Probe() = %#v", status)
	}
}

func TestLarkIMAuthorizationUsesDeviceFlow(t *testing.T) {
	var commands []string
	authorizer := newAuthorizer(fakeRunner{run: func(bin string, args []string) ([]byte, error) {
		command := bin + " " + strings.Join(args, " ")
		commands = append(commands, command)
		switch {
		case strings.Contains(command, "im +chat-search"):
			return []byte(`{"ok":false,"error":{"type":"authorization","subtype":"missing_scope","code":99991679,"message":"login required"}}`), errors.New("exit 1")
		case strings.Contains(command, "--no-wait"):
			return []byte(`{"data":{"device_code":"device-1","verification_uri":"https://example.test/login","user_code":"ABCD"}}`), nil
		case strings.Contains(command, "--device-code"):
			return []byte(`{"status":"success"}`), nil
		default:
			return nil, fmt.Errorf("unexpected command: %s", command)
		}
	}})
	begin := authorizer.Begin(t.Context(), "lark-cli-im")
	if begin.Status != AuthPending || begin.FlowID == nil {
		t.Fatalf("begin = %#v", begin)
	}
	complete := authorizer.Complete(t.Context(), "lark-cli-im", *begin.FlowID)
	if complete.Status != AuthAuthorized {
		t.Fatalf("complete = %#v", complete)
	}
	if len(commands) != 3 || !strings.HasPrefix(commands[1], "lark-cli auth login") ||
		!strings.Contains(commands[2], "--device-code device-1") {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestFindStringSearchesNestedAuthorizationPayload(t *testing.T) {
	var payload any
	if err := json.Unmarshal([]byte(`{"data":{"challenge":{"device_code":"x"}}}`), &payload); err != nil {
		t.Fatal(err)
	}
	if got := findString(payload, "device_code"); got != "x" {
		t.Fatalf("findString() = %q", got)
	}
}

func TestHasErrorCodeChecksSubtypeIndependentlyFromStringCode(t *testing.T) {
	raw := []byte(`{"ok":false,"error":{"type":"authorization","subtype":"missing_scope","code":"99991679"}}`)
	if !hasErrorCode(raw, "MISSING_SCOPE") {
		t.Fatal("hasErrorCode() did not match lark-cli error.subtype")
	}
}

func TestCommandErrorPrefersErrorOverNoticeMessage(t *testing.T) {
	raw := []byte(`{"ok":false,"error":{"message":"missing scope"},"_notice":{"update":{"message":"new version available"}}}`)
	if got := commandError(raw, errors.New("exit 1")); got != "missing scope" {
		t.Fatalf("commandError() = %q", got)
	}
}
