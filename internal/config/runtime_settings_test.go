package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const runtimeSettingsTestYAML = `
server:
  addr: "127.0.0.1:18800"
  web_root: "web/dist"
mysql:
  dsn: "user:pass@tcp(127.0.0.1:3306)/jarvis"
  max_open_conns: 20
  max_idle_conns: 5
  conn_max_lifetime: 3600
mem0:
  base_url: "http://127.0.0.1:18900"
  owner_id: "owner"
  timeout_sec: 60
  batch_limit: 400
  window_gap_minutes: 30
  window_max_messages: 40
  schedule: "@every 10m"
  qdrant_host: "127.0.0.1"
  qdrant_port: 6333
  qdrant_grpc_port: 6334
  collection: "jarvis_memories"
  state_dir: "var/mem0"
  embedding_model: "embed-model"
  embedding_dims: 1024
model:
  base_url: "https://model.test/v1"
  api_key: "plain-key"
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
  batch_messages: 400
  context_messages: 20
  context_window_minutes: 120
  open_todo_limit: 50
  memory_top_k: 8
  memory_threshold: 0.5
  max_prompt_chars: 60000
  semantic_collection: "todo_semantic"
  semantic_threshold: 0.85
  semantic_neighbor_limit: 3
  tool_timeout_sec: 10
  history_tool_limit: 50
  tool_memory_max_top_k: 20
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
decide:
  enabled: true
  mode: "codex"
  schedule: "@every 1m"
  batch_limit: 50
  codex_sandbox: "danger-full-access"
  codex_network: true
  codex_reasoning_effort: "medium"
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
  group_message_limit: 200
  group_concurrency: 2
scheduled_task:
  enabled: true
  schedule: "@every 1m"
  batch_limit: 20
`

func TestRuntimeSettingsUpdateWritesOverlayAndRequiresRestart(t *testing.T) {
	configPath := writeRuntimeSettingsTestConfig(t)
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
	input.CaptureScanWorkers = 6
	input.MemoryWindowMaxMessages = 80
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
		updated.Settings.ExecuteConcurrency != 4 || updated.Settings.ExtractSchedule != "@every 2m" ||
		updated.Settings.CaptureScanWorkers != 6 || updated.Settings.MemoryWindowMaxMessages != 80 ||
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
		reloaded.Execute.Concurrency != 4 || reloaded.Extract.Schedule != "@every 2m" ||
		reloaded.Capture.ScanWorkers != 6 || reloaded.Mem0.WindowMaxMessages != 80 ||
		reloaded.LarkCLI.RateLimit != 7.5 || reloaded.DailyDigest.GroupConcurrency != 4 {
		t.Fatalf("reloaded config = %#v", reloaded)
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

func writeRuntimeSettingsTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(runtimeSettingsTestYAML), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	return path
}
