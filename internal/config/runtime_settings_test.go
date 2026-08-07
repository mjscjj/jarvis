package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const runtimeSettingsTestYAML = `
server:
  addr: "0.0.0.0:18800"
  web_root: "web/dist"
sqlite:
  path: "var/jarvis.db"
factengine:
  enabled: true
  schedule: "@every 15m"
  rollup_schedule: "0 2 * * *"
  bin: "traex"
  model: "fixture-fact-model"
  rollup_model: "fixture-rollup-model"
  sandbox: "danger-full-access"
  timeout_sec: 300
  batch_limit: 200
  max_material_chars: 100000
  window_gap_minutes: 30
  window_max_messages: 40
proactive:
  enabled: true
  schedule: "@every 1h"
  startup_delay_seconds: 120
  bin: "traex"
  model: "DeepSeek-V4-Pro"
  sandbox: "danger-full-access"
  reasoning_effort: "medium"
  timeout_seconds: 900
meeting_sweep:
  enabled: true
  schedule: "@every 2h"
  startup_delay_seconds: 150
  bin: "traex"
  model: "DeepSeek-V4-Flash"
  sandbox: "danger-full-access"
  reasoning_effort: "low"
  timeout_seconds: 600
morning_brief:
  enabled: true
  schedule: "30 8 * * 1-5"
  startup_delay_seconds: 180
  bin: "traex"
  model: "gpt-5.6-sol"
  sandbox: "danger-full-access"
  reasoning_effort: "medium"
  timeout_seconds: 600
model:
  model: "model"
  timeout_sec: 60
extract:
  enabled: true
  principal_open_id: "ou_owner"
  schedule: "@every 10m"
  engine: "codex"
  codex_sandbox: "danger-full-access"
  codex_network: true
  codex_reasoning_effort: "low"
  concurrency: 2
  batch_messages: 400
  context_messages: 20
  context_window_minutes: 120
  open_todo_limit: 50
  fact_limit: 10
  key_person_limit: 5
  recent_task_limit: 10
  max_prompt_chars: 60000
  semantic_collection: "todo_semantic"
  semantic_threshold: 0.85
  semantic_neighbor_limit: 3
  tool_timeout_sec: 10
  history_tool_limit: 50
  qdrant_host: "127.0.0.1"
  qdrant_grpc_port: 6334
lark_cli:
  bin: "lark-cli"
  rate_limit: 5
  burst: 10
  concurrent: 2
  timeout_sec: 60
capture:
  page_size: 50
  scan_workers: 2
  hot_age_hours: 6
  warm_age_hours: 168
  timezone: "Asia/Shanghai"
  discover_schedule: "@every 6h"
  scan_schedule: "@every 5m"
codex:
  bin: "traex"
  model: "analysis-model"
  timeout_seconds: 600
execute:
  enabled: true
  schedule: "@every 5m"
  batch_limit: 5
  concurrency: 3
  repo_root: "/tmp/repos"
  runs_dir: "/tmp/runs"
  bin: "codex"
  model: "execution-model"
  reasoning_effort: "medium"
  timeout_second: 1800
  stale_executing_minute: 45
chat:
  enabled: true
  model: "chat-model"
  timeout_seconds: 600
  sandbox: "danger-full-access"
  reasoning_effort: "medium"
skills:
  root: ".agents/skills"
dailydigest:
  enabled: true
  schedule: "0 19 * * *"
  timeout_seconds: 600
  git_author: "owner@example.com"
  group_message_limit: 200
  group_concurrency: 2
scheduled_task:
  enabled: true
  schedule: "@every 1m"
  batch_limit: 20
`

func TestRuntimeSettingsUpdateWritesOverlayAndRequiresRestart(t *testing.T) {
	configPath := writeRuntimeSettingsTestConfig(t)
	if err := os.WriteFile(RuntimeOverridePath(configPath), []byte(`
card_approval:
  enabled: true
  profile: cli_jarvis
  principal_open_id: ou_principal
  relay_secret: relay-secret
extract:
  principal_open_id: ou_initialized
lark_cli:
  bin: custom-lark-cli
  profile: cli_initialized
dailydigest:
  git_author: initialized@example.com
`), 0o600); err != nil {
		t.Fatalf("write card approval runtime override: %v", err)
	}
	baseBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}
	active, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	service, err := NewRuntimeSettingsService(configPath, active)
	if err != nil {
		t.Fatalf("NewRuntimeSettingsService() error = %v", err)
	}
	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if view.RestartRequired {
		t.Fatal("fresh service unexpectedly requires restart")
	}

	input := view.Settings
	input.AnalysisCLI = "codex"
	input.AnalysisModel = "new-analysis-model"
	input.ExecuteCLI = "traex"
	input.ExecuteConcurrency = 4
	input.ExtractSchedule = "@every 2m"
	input.ExtractConcurrency = 4
	input.CaptureScanWorkers = 6
	input.FactEngineWindowMaxMessages = 80
	input.ProactiveSchedule = "@every 2h"
	input.ProactiveStartupDelaySeconds = 180
	input.LarkRateLimit = 7.5
	input.DailyDigestConcurrency = 4
	updated, err := service.Update(context.Background(), input)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !updated.RestartRequired {
		t.Fatal("updated settings should require restart")
	}
	if !reflect.DeepEqual(updated.Settings, input) {
		t.Fatalf("round-trip settings mismatch:\nupdated=%#v\ninput=%#v", updated.Settings, input)
	}
	if updated.Settings.AnalysisCLI != "codex" || updated.Settings.ExecuteCLI != "traex" ||
		updated.Settings.ExecuteConcurrency != 4 || updated.Settings.ExtractSchedule != "@every 2m" || updated.Settings.ExtractConcurrency != 4 ||
		updated.Settings.CaptureScanWorkers != 6 || updated.Settings.FactEngineWindowMaxMessages != 80 ||
		updated.Settings.ProactiveSchedule != "@every 2h" || updated.Settings.ProactiveStartupDelaySeconds != 180 ||
		updated.Settings.LarkRateLimit != 7.5 || updated.Settings.DailyDigestConcurrency != 4 {
		t.Fatalf("updated settings = %#v", updated.Settings)
	}
	baseAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read base config after update: %v", err)
	}
	if string(baseAfter) != string(baseBefore) {
		t.Fatal("base config was modified")
	}
	if _, err := os.Stat(RuntimeOverridePath(configPath)); err != nil {
		t.Fatalf("runtime override stat: %v", err)
	}

	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after update error = %v", err)
	}
	if reloaded.Codex.Bin != "codex" || reloaded.Execute.Bin != "traex" ||
		reloaded.Execute.Concurrency != 4 || reloaded.Extract.Schedule != "@every 2m" || reloaded.Extract.Concurrency != 4 ||
		reloaded.Capture.ScanWorkers != 6 || reloaded.FactEngine.WindowMaxMessages != 80 ||
		reloaded.Proactive.Schedule != "@every 2h" || reloaded.Proactive.StartupDelaySeconds != 180 ||
		reloaded.LarkCLI.RateLimit != 7.5 || reloaded.DailyDigest.GroupConcurrency != 4 {
		t.Fatalf("reloaded config = %#v", reloaded)
	}
	if got := reloaded.CardApproval; !got.Enabled ||
		got.Profile != "cli_jarvis" || got.PrincipalOpenID != "ou_principal" ||
		got.RelaySecret != "relay-secret" {
		t.Fatalf("card approval config was not preserved: %#v", got)
	}
	if reloaded.Extract.PrincipalOpenID != "ou_initialized" ||
		reloaded.LarkCLI.Bin != "custom-lark-cli" || reloaded.LarkCLI.Profile != "cli_initialized" ||
		reloaded.DailyDigest.GitAuthor != "initialized@example.com" {
		t.Fatalf("initialization identity config was not preserved: extract=%q lark=%#v dailydigest=%#v", reloaded.Extract.PrincipalOpenID, reloaded.LarkCLI, reloaded.DailyDigest)
	}
	restartedService, err := NewRuntimeSettingsService(configPath, reloaded)
	if err != nil {
		t.Fatalf("NewRuntimeSettingsService(reloaded) error = %v", err)
	}
	restartedView, err := restartedService.Get(context.Background())
	if err != nil {
		t.Fatalf("restarted Get() error = %v", err)
	}
	if restartedView.RestartRequired {
		t.Fatal("service created from reloaded config unexpectedly requires restart")
	}
}

func TestRuntimeSettingsUpdateRejectsInvalidDependency(t *testing.T) {
	configPath := writeRuntimeSettingsTestConfig(t)
	active, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	service, err := NewRuntimeSettingsService(configPath, active)
	if err != nil {
		t.Fatalf("NewRuntimeSettingsService() error = %v", err)
	}
	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	input := view.Settings
	input.ExecuteAutoEnabled = false
	input.ScheduledTaskEnabled = true
	if _, err := service.Update(context.Background(), input); !errors.Is(err, ErrInvalidRuntimeSettings) {
		t.Fatalf("Update() error = %v, want ErrInvalidRuntimeSettings", err)
	}
	if _, err := os.Stat(RuntimeOverridePath(configPath)); !os.IsNotExist(err) {
		t.Fatalf("invalid update wrote override: %v", err)
	}
}

func TestRuntimeSettingsUpdateRejectsInvalidSchedule(t *testing.T) {
	configPath := writeRuntimeSettingsTestConfig(t)
	active, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	service, err := NewRuntimeSettingsService(configPath, active)
	if err != nil {
		t.Fatalf("NewRuntimeSettingsService() error = %v", err)
	}
	view, err := service.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	input := view.Settings
	input.CaptureScanSchedule = "not-a-schedule"
	if _, err := service.Update(context.Background(), input); !errors.Is(err, ErrInvalidRuntimeSettings) {
		t.Fatalf("Update() error = %v, want ErrInvalidRuntimeSettings", err)
	}
	if _, err := os.Stat(RuntimeOverridePath(configPath)); !os.IsNotExist(err) {
		t.Fatalf("invalid update wrote override: %v", err)
	}
}

func TestLoadRejectsRetiredDecideSection(t *testing.T) {
	configPath := writeRuntimeSettingsTestConfig(t)
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	raw = append(raw, []byte("\ndecide:\n  enabled: true\n")...)
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatalf("write config with retired decide section: %v", err)
	}
	if _, err := Load(configPath); err == nil || !strings.Contains(err.Error(), "field decide not found") {
		t.Fatalf("Load() error = %v, want retired decide field rejection", err)
	}
}

func writeRuntimeSettingsTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(runtimeSettingsTestYAML), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	return path
}
