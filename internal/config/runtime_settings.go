package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const runtimeOverrideFilename = "config.runtime.yaml"

var ErrInvalidRuntimeSettings = errors.New("invalid runtime settings")

// RuntimeSettings is the user-editable runtime subset of config. The assistant
// display name is editable here; principal identity, secrets, service endpoints,
// and infrastructure paths remain outside the settings page.
type RuntimeSettings struct {
	AgentDisplayName       string `json:"agent_display_name"`
	AnalysisCLI            string `json:"analysis_cli"`
	AnalysisModel          string `json:"analysis_model"`
	AnalysisTimeoutSeconds int    `json:"analysis_timeout_seconds"`
	ModelAPIModel          string `json:"model_api_model"`
	ModelAPITimeoutSeconds int    `json:"model_api_timeout_seconds"`

	ExtractEnabled               bool    `json:"extract_enabled"`
	ExtractEngine                string  `json:"extract_engine"`
	ExtractSchedule              string  `json:"extract_schedule"`
	ExtractConcurrency           int     `json:"extract_concurrency"`
	ExtractBatchMessages         int     `json:"extract_batch_messages"`
	ExtractSandbox               string  `json:"extract_sandbox"`
	ExtractNetworkEnabled        bool    `json:"extract_network_enabled"`
	ExtractReasoningEffort       string  `json:"extract_reasoning_effort"`
	ExtractContextMessages       int     `json:"extract_context_messages"`
	ExtractContextWindowMinutes  int     `json:"extract_context_window_minutes"`
	ExtractMaxPromptChars        int     `json:"extract_max_prompt_chars"`
	ExtractSemanticThreshold     float64 `json:"extract_semantic_threshold"`
	ExtractSemanticNeighborLimit int     `json:"extract_semantic_neighbor_limit"`
	ExtractToolTimeoutSeconds    int     `json:"extract_tool_timeout_seconds"`
	ExtractHistoryToolLimit      int     `json:"extract_history_tool_limit"`
	ExtractEvidenceRetryMax      int     `json:"extract_evidence_retry_max"`

	ExecuteAutoEnabled     bool   `json:"execute_auto_enabled"`
	ExecuteCLI             string `json:"execute_cli"`
	ExecuteModel           string `json:"execute_model"`
	ExecuteReasoningEffort string `json:"execute_reasoning_effort"`
	ExecuteSchedule        string `json:"execute_schedule"`
	ExecuteBatchLimit      int    `json:"execute_batch_limit"`
	ExecuteTimeoutSeconds  int    `json:"execute_timeout_seconds"`
	ExecuteStaleMinutes    int    `json:"execute_stale_minutes"`
	ExecuteConcurrency     int    `json:"execute_concurrency"`

	ChatModel           string `json:"chat_model"`
	ChatSandbox         string `json:"chat_sandbox"`
	ChatReasoningEffort string `json:"chat_reasoning_effort"`
	ChatTimeoutSeconds  int    `json:"chat_timeout_seconds"`

	CapturePageSize           int    `json:"capture_page_size"`
	CaptureScanWorkers        int    `json:"capture_scan_workers"`
	CaptureDiscoverSchedule   string `json:"capture_discover_schedule"`
	CaptureScanSchedule       string `json:"capture_scan_schedule"`
	CaptureP2PWindowMinutes   int    `json:"capture_p2p_activation_window_minutes"`
	CaptureAutoRelatedP2PTopN int    `json:"capture_auto_related_p2p_top_n"`

	FactEngineEnabled           bool   `json:"fact_engine_enabled"`
	FactEngineSchedule          string `json:"fact_engine_schedule"`
	FactEngineModel             string `json:"fact_engine_model"`
	FactEngineReasoningEffort   string `json:"fact_engine_reasoning_effort"`
	FactEngineTimeoutSeconds    int    `json:"fact_engine_timeout_seconds"`
	FactEngineBatchLimit        int    `json:"fact_engine_batch_limit"`
	FactEngineMaxMaterialChars  int    `json:"fact_engine_max_material_chars"`
	FactEngineWindowGapMinutes  int    `json:"fact_engine_window_gap_minutes"`
	FactEngineWindowMaxMessages int    `json:"fact_engine_window_max_messages"`

	ProactiveEnabled             bool   `json:"proactive_enabled"`
	ProactiveSchedule            string `json:"proactive_schedule"`
	ProactiveStartupDelaySeconds int    `json:"proactive_startup_delay_seconds"`
	ProactiveCLI                 string `json:"proactive_cli"`
	ProactiveModel               string `json:"proactive_model"`
	ProactiveSandbox             string `json:"proactive_sandbox"`
	ProactiveReasoningEffort     string `json:"proactive_reasoning_effort"`
	ProactiveTimeoutSeconds      int    `json:"proactive_timeout_seconds"`

	LarkRateLimit      float64 `json:"lark_rate_limit"`
	LarkBurst          int     `json:"lark_burst"`
	LarkConcurrency    int     `json:"lark_concurrency"`
	LarkTimeoutSeconds int     `json:"lark_timeout_seconds"`

	ScheduledTaskEnabled    bool   `json:"scheduled_task_enabled"`
	ScheduledTaskSchedule   string `json:"scheduled_task_schedule"`
	ScheduledTaskBatchLimit int    `json:"scheduled_task_batch_limit"`
	DailyDigestEnabled      bool   `json:"daily_digest_enabled"`
	DailyDigestSchedule     string `json:"daily_digest_schedule"`
	DailyDigestMessageLimit int    `json:"daily_digest_message_limit"`
	DailyDigestConcurrency  int    `json:"daily_digest_concurrency"`
}

type RuntimeSettingsView struct {
	Settings        RuntimeSettings `json:"settings"`
	RestartRequired bool            `json:"restart_required"`
	OverridePath    string          `json:"override_path"`
}

type SecuritySettings struct {
	P2PScanEnabled     bool `json:"p2p_scan_enabled"`
	AutoRelatedP2PTopN int  `json:"auto_related_p2p_top_n"`
}

type SecurityCapability struct {
	Enforceable bool   `json:"enforceable"`
	Enabled     bool   `json:"enabled"`
	Message     string `json:"message"`
}

type SecuritySettingsView struct {
	Settings        SecuritySettings   `json:"settings"`
	RestartRequired bool               `json:"restart_required"`
	L4DocumentRead  SecurityCapability `json:"l4_document_read"`
}

// RuntimeSettingsService persists a local overlay next to the main config.
// The active snapshot is frozen at process start, so the API can truthfully
// report whether saved settings differ from the running process.
type RuntimeSettingsService struct {
	mu                       sync.Mutex
	configPath               string
	active                   RuntimeSettings
	activeP2PScanEnabled     bool
	activeAutoRelatedP2PTopN int
}

func NewRuntimeSettingsService(configPath string, active *Config) (*RuntimeSettingsService, error) {
	if strings.TrimSpace(configPath) == "" {
		return nil, fmt.Errorf("runtime settings config path is empty")
	}
	if active == nil {
		return nil, fmt.Errorf("runtime settings active config is nil")
	}
	return &RuntimeSettingsService{
		configPath:               configPath,
		active:                   runtimeSettingsFromConfig(active),
		activeP2PScanEnabled:     active.Capture.P2PScanEnabled,
		activeAutoRelatedP2PTopN: active.Capture.AutoRelatedP2PTopN,
	}, nil
}

func RuntimeOverridePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), runtimeOverrideFilename)
}

func (s *RuntimeSettingsService) Get(ctx context.Context) (*RuntimeSettingsView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked()
}

func (s *RuntimeSettingsService) Update(ctx context.Context, input RuntimeSettings) (*RuntimeSettingsView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := Load(s.configPath)
	if err != nil {
		return nil, err
	}
	applyRuntimeSettings(cfg, input)
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeSettings, err)
	}
	if err := UpdateRuntimeOverride(s.configPath, runtimeOverrideFromSettings(input)); err != nil {
		return nil, err
	}
	return s.getLocked()
}

func (s *RuntimeSettingsService) GetSecurity(ctx context.Context) (*SecuritySettingsView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := Load(s.configPath)
	if err != nil {
		return nil, err
	}
	return s.securityView(cfg), nil
}

func (s *RuntimeSettingsService) UpdateSecurity(ctx context.Context, input SecuritySettings) (*SecuritySettingsView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := Load(s.configPath)
	if err != nil {
		return nil, err
	}
	cfg.Capture.P2PScanEnabled = input.P2PScanEnabled
	cfg.Capture.AutoRelatedP2PTopN = input.AutoRelatedP2PTopN
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRuntimeSettings, err)
	}
	if err := UpdateRuntimeOverride(s.configPath, map[string]any{
		"capture": map[string]any{
			"p2p_scan_enabled": input.P2PScanEnabled,
			"auto_related_p2p_top_n": input.AutoRelatedP2PTopN,
		},
	}); err != nil {
		return nil, err
	}
	reloaded, err := Load(s.configPath)
	if err != nil {
		return nil, err
	}
	return s.securityView(reloaded), nil
}

func (s *RuntimeSettingsService) securityView(cfg *Config) *SecuritySettingsView {
	return &SecuritySettingsView{
		Settings: SecuritySettings{
			P2PScanEnabled: cfg.Capture.P2PScanEnabled, AutoRelatedP2PTopN: cfg.Capture.AutoRelatedP2PTopN,
		},
		RestartRequired: cfg.Capture.P2PScanEnabled != s.activeP2PScanEnabled ||
			cfg.Capture.AutoRelatedP2PTopN != s.activeAutoRelatedP2PTopN,
		L4DocumentRead: SecurityCapability{
			Enforceable: false,
			Enabled:     false,
			Message:     "当前 lark-cli 没有统一的读取前密级拦截能力，暂不提供虚假的开关。",
		},
	}
}

func (s *RuntimeSettingsService) getLocked() (*RuntimeSettingsView, error) {
	cfg, err := Load(s.configPath)
	if err != nil {
		return nil, err
	}
	settings := runtimeSettingsFromConfig(cfg)
	return &RuntimeSettingsView{
		Settings:        settings,
		RestartRequired: !reflect.DeepEqual(settings, s.active),
		OverridePath:    RuntimeOverridePath(s.configPath),
	}, nil
}

func runtimeSettingsFromConfig(cfg *Config) RuntimeSettings {
	return RuntimeSettings{
		AgentDisplayName:             cfg.Identity.DisplayName,
		AnalysisCLI:                  cfg.Codex.Bin,
		AnalysisModel:                cfg.Codex.Model,
		AnalysisTimeoutSeconds:       cfg.Codex.TimeoutSeconds,
		ModelAPIModel:                cfg.Model.Model,
		ModelAPITimeoutSeconds:       cfg.Model.TimeoutSec,
		ExtractEnabled:               cfg.Extract.Enabled,
		ExtractEngine:                cfg.Extract.Engine,
		ExtractSchedule:              cfg.Extract.Schedule,
		ExtractConcurrency:           cfg.Extract.Concurrency,
		ExtractBatchMessages:         cfg.Extract.BatchMessages,
		ExtractSandbox:               cfg.Extract.CodexSandbox,
		ExtractNetworkEnabled:        cfg.Extract.CodexNetwork,
		ExtractReasoningEffort:       cfg.Extract.CodexReasoningEffort,
		ExtractContextMessages:       cfg.Extract.ContextMessages,
		ExtractContextWindowMinutes:  cfg.Extract.ContextWindowMinutes,
		ExtractMaxPromptChars:        cfg.Extract.MaxPromptChars,
		ExtractSemanticThreshold:     cfg.Extract.SemanticThreshold,
		ExtractSemanticNeighborLimit: cfg.Extract.SemanticNeighborLimit,
		ExtractToolTimeoutSeconds:    cfg.Extract.ToolTimeoutSec,
		ExtractHistoryToolLimit:      cfg.Extract.HistoryToolLimit,
		ExtractEvidenceRetryMax:      cfg.Extract.EvidenceRetryMax,
		ExecuteAutoEnabled:           cfg.Execute.Enabled,
		ExecuteCLI:                   cfg.Execute.Bin,
		ExecuteModel:                 cfg.Execute.Model,
		ExecuteReasoningEffort:       cfg.Execute.ReasoningEffort,
		ExecuteSchedule:              cfg.Execute.Schedule,
		ExecuteBatchLimit:            cfg.Execute.BatchLimit,
		ExecuteTimeoutSeconds:        cfg.Execute.TimeoutSecond,
		ExecuteStaleMinutes:          cfg.Execute.StaleExecutingMinute,
		ExecuteConcurrency:           cfg.Execute.Concurrency,
		ChatModel:                    cfg.Chat.Model,
		ChatSandbox:                  cfg.Chat.Sandbox,
		ChatReasoningEffort:          cfg.Chat.ReasoningEffort,
		ChatTimeoutSeconds:           cfg.Chat.TimeoutSeconds,
		CapturePageSize:              cfg.Capture.PageSize,
		CaptureScanWorkers:           cfg.Capture.ScanWorkers,
		CaptureDiscoverSchedule:      cfg.Capture.DiscoverSchedule,
		CaptureScanSchedule:          cfg.Capture.ScanSchedule,
		CaptureP2PWindowMinutes:      cfg.Capture.P2PWindowMinutes,
		CaptureAutoRelatedP2PTopN:    cfg.Capture.AutoRelatedP2PTopN,
		FactEngineEnabled:            cfg.FactEngine.Enabled,
		FactEngineSchedule:           cfg.FactEngine.Schedule,
		FactEngineModel:              cfg.FactEngine.Model,
		FactEngineReasoningEffort:    cfg.FactEngine.ReasoningEffort,
		FactEngineTimeoutSeconds:     cfg.FactEngine.TimeoutSec,
		FactEngineBatchLimit:         cfg.FactEngine.BatchLimit,
		FactEngineMaxMaterialChars:   cfg.FactEngine.MaxMaterialChars,
		FactEngineWindowGapMinutes:   cfg.FactEngine.WindowGapMinutes,
		FactEngineWindowMaxMessages:  cfg.FactEngine.WindowMaxMessages,
		ProactiveEnabled:             cfg.Proactive.Enabled,
		ProactiveSchedule:            cfg.Proactive.Schedule,
		ProactiveStartupDelaySeconds: cfg.Proactive.StartupDelaySeconds,
		ProactiveCLI:                 cfg.Proactive.Bin,
		ProactiveModel:               cfg.Proactive.Model,
		ProactiveSandbox:             cfg.Proactive.Sandbox,
		ProactiveReasoningEffort:     cfg.Proactive.ReasoningEffort,
		ProactiveTimeoutSeconds:      cfg.Proactive.TimeoutSeconds,
		LarkRateLimit:                cfg.LarkCLI.RateLimit,
		LarkBurst:                    cfg.LarkCLI.Burst,
		LarkConcurrency:              cfg.LarkCLI.Concurrent,
		LarkTimeoutSeconds:           cfg.LarkCLI.TimeoutSec,
		ScheduledTaskEnabled:         cfg.ScheduledTask.Enabled,
		ScheduledTaskSchedule:        cfg.ScheduledTask.Schedule,
		ScheduledTaskBatchLimit:      cfg.ScheduledTask.BatchLimit,
		DailyDigestEnabled:           cfg.DailyDigest.Enabled,
		DailyDigestSchedule:          cfg.DailyDigest.Schedule,
		DailyDigestMessageLimit:      cfg.DailyDigest.GroupMessageLimit,
		DailyDigestConcurrency:       cfg.DailyDigest.GroupConcurrency,
	}
}

func applyRuntimeSettings(cfg *Config, input RuntimeSettings) {
	cfg.Identity.DisplayName = strings.TrimSpace(input.AgentDisplayName)
	cfg.Codex.Bin = strings.TrimSpace(input.AnalysisCLI)
	cfg.Codex.Model = strings.TrimSpace(input.AnalysisModel)
	cfg.Codex.TimeoutSeconds = input.AnalysisTimeoutSeconds
	cfg.Model.Model = strings.TrimSpace(input.ModelAPIModel)
	cfg.Model.TimeoutSec = input.ModelAPITimeoutSeconds
	cfg.Extract.Enabled = input.ExtractEnabled
	cfg.Extract.Engine = strings.TrimSpace(input.ExtractEngine)
	cfg.Extract.Schedule = strings.TrimSpace(input.ExtractSchedule)
	cfg.Extract.Concurrency = input.ExtractConcurrency
	cfg.Extract.BatchMessages = input.ExtractBatchMessages
	cfg.Extract.CodexSandbox = strings.TrimSpace(input.ExtractSandbox)
	cfg.Extract.CodexNetwork = input.ExtractNetworkEnabled
	cfg.Extract.CodexReasoningEffort = strings.TrimSpace(input.ExtractReasoningEffort)
	cfg.Extract.ContextMessages = input.ExtractContextMessages
	cfg.Extract.ContextWindowMinutes = input.ExtractContextWindowMinutes
	cfg.Extract.MaxPromptChars = input.ExtractMaxPromptChars
	cfg.Extract.SemanticThreshold = input.ExtractSemanticThreshold
	cfg.Extract.SemanticNeighborLimit = input.ExtractSemanticNeighborLimit
	cfg.Extract.ToolTimeoutSec = input.ExtractToolTimeoutSeconds
	cfg.Extract.HistoryToolLimit = input.ExtractHistoryToolLimit
	cfg.Extract.EvidenceRetryMax = input.ExtractEvidenceRetryMax
	cfg.Execute.Enabled = input.ExecuteAutoEnabled
	cfg.Execute.Bin = strings.TrimSpace(input.ExecuteCLI)
	cfg.Execute.Model = strings.TrimSpace(input.ExecuteModel)
	cfg.Execute.ReasoningEffort = strings.TrimSpace(input.ExecuteReasoningEffort)
	cfg.Execute.Schedule = strings.TrimSpace(input.ExecuteSchedule)
	cfg.Execute.BatchLimit = input.ExecuteBatchLimit
	cfg.Execute.TimeoutSecond = input.ExecuteTimeoutSeconds
	cfg.Execute.StaleExecutingMinute = input.ExecuteStaleMinutes
	cfg.Execute.Concurrency = input.ExecuteConcurrency
	cfg.Chat.Model = strings.TrimSpace(input.ChatModel)
	cfg.Chat.Sandbox = strings.TrimSpace(input.ChatSandbox)
	cfg.Chat.ReasoningEffort = strings.TrimSpace(input.ChatReasoningEffort)
	cfg.Chat.TimeoutSeconds = input.ChatTimeoutSeconds
	cfg.Capture.PageSize = input.CapturePageSize
	cfg.Capture.ScanWorkers = input.CaptureScanWorkers
	cfg.Capture.DiscoverSchedule = strings.TrimSpace(input.CaptureDiscoverSchedule)
	cfg.Capture.ScanSchedule = strings.TrimSpace(input.CaptureScanSchedule)
	cfg.Capture.P2PWindowMinutes = input.CaptureP2PWindowMinutes
	cfg.FactEngine.Enabled = input.FactEngineEnabled
	cfg.FactEngine.Schedule = strings.TrimSpace(input.FactEngineSchedule)
	cfg.FactEngine.Model = strings.TrimSpace(input.FactEngineModel)
	cfg.FactEngine.ReasoningEffort = strings.TrimSpace(input.FactEngineReasoningEffort)
	cfg.FactEngine.TimeoutSec = input.FactEngineTimeoutSeconds
	cfg.FactEngine.BatchLimit = input.FactEngineBatchLimit
	cfg.FactEngine.MaxMaterialChars = input.FactEngineMaxMaterialChars
	cfg.FactEngine.WindowGapMinutes = input.FactEngineWindowGapMinutes
	cfg.FactEngine.WindowMaxMessages = input.FactEngineWindowMaxMessages
	cfg.Proactive.Enabled = input.ProactiveEnabled
	cfg.Proactive.Schedule = strings.TrimSpace(input.ProactiveSchedule)
	cfg.Proactive.StartupDelaySeconds = input.ProactiveStartupDelaySeconds
	cfg.Proactive.Bin = strings.TrimSpace(input.ProactiveCLI)
	cfg.Proactive.Model = strings.TrimSpace(input.ProactiveModel)
	cfg.Proactive.Sandbox = strings.TrimSpace(input.ProactiveSandbox)
	cfg.Proactive.ReasoningEffort = strings.TrimSpace(input.ProactiveReasoningEffort)
	cfg.Proactive.TimeoutSeconds = input.ProactiveTimeoutSeconds
	cfg.LarkCLI.RateLimit = input.LarkRateLimit
	cfg.LarkCLI.Burst = input.LarkBurst
	cfg.LarkCLI.Concurrent = input.LarkConcurrency
	cfg.LarkCLI.TimeoutSec = input.LarkTimeoutSeconds
	cfg.ScheduledTask.Enabled = input.ScheduledTaskEnabled
	cfg.ScheduledTask.Schedule = strings.TrimSpace(input.ScheduledTaskSchedule)
	cfg.ScheduledTask.BatchLimit = input.ScheduledTaskBatchLimit
	cfg.DailyDigest.Enabled = input.DailyDigestEnabled
	cfg.DailyDigest.Schedule = strings.TrimSpace(input.DailyDigestSchedule)
	cfg.DailyDigest.GroupMessageLimit = input.DailyDigestMessageLimit
	cfg.DailyDigest.GroupConcurrency = input.DailyDigestConcurrency
}

type runtimeOverride struct {
	Identity IdentityConfig `yaml:"identity"`
	Extract struct {
		Enabled               bool    `yaml:"enabled"`
		Engine                string  `yaml:"engine"`
		Schedule              string  `yaml:"schedule"`
		Concurrency           int     `yaml:"concurrency"`
		BatchMessages         int     `yaml:"batch_messages"`
		Sandbox               string  `yaml:"codex_sandbox"`
		Network               bool    `yaml:"codex_network"`
		ReasoningEffort       string  `yaml:"codex_reasoning_effort"`
		ContextMessages       int     `yaml:"context_messages"`
		ContextWindowMinutes  int     `yaml:"context_window_minutes"`
		MaxPromptChars        int     `yaml:"max_prompt_chars"`
		SemanticThreshold     float64 `yaml:"semantic_threshold"`
		SemanticNeighborLimit int     `yaml:"semantic_neighbor_limit"`
		ToolTimeoutSec        int     `yaml:"tool_timeout_sec"`
		HistoryToolLimit      int     `yaml:"history_tool_limit"`
		EvidenceRetryMax      int     `yaml:"evidence_retry_max"`
	} `yaml:"extract"`
	Model struct {
		Model      string `yaml:"model"`
		TimeoutSec int    `yaml:"timeout_sec"`
	} `yaml:"model"`
	Codex struct {
		Bin            string `yaml:"bin"`
		Model          string `yaml:"model"`
		TimeoutSeconds int    `yaml:"timeout_seconds"`
	} `yaml:"codex"`
	Execute struct {
		Enabled              bool   `yaml:"enabled"`
		Bin                  string `yaml:"bin"`
		Model                string `yaml:"model"`
		ReasoningEffort      string `yaml:"reasoning_effort"`
		Schedule             string `yaml:"schedule"`
		BatchLimit           int    `yaml:"batch_limit"`
		TimeoutSecond        int    `yaml:"timeout_second"`
		StaleExecutingMinute int    `yaml:"stale_executing_minute"`
		Concurrency          int    `yaml:"concurrency"`
	} `yaml:"execute"`
	Chat struct {
		Model           string `yaml:"model"`
		Sandbox         string `yaml:"sandbox"`
		ReasoningEffort string `yaml:"reasoning_effort"`
		TimeoutSeconds  int    `yaml:"timeout_seconds"`
	} `yaml:"chat"`
	Capture struct {
		PageSize                   int    `yaml:"page_size"`
		ScanWorkers                int    `yaml:"scan_workers"`
		DiscoverSchedule           string `yaml:"discover_schedule"`
		ScanSchedule               string `yaml:"scan_schedule"`
		P2PActivationWindowMinutes int    `yaml:"p2p_activation_window_minutes"`
	} `yaml:"capture"`
	FactEngine   struct {
		Enabled           bool   `yaml:"enabled"`
		Schedule          string `yaml:"schedule"`
		Model             string `yaml:"model"`
		ReasoningEffort   string `yaml:"reasoning_effort"`
		TimeoutSec        int    `yaml:"timeout_sec"`
		BatchLimit        int    `yaml:"batch_limit"`
		MaxMaterialChars  int    `yaml:"max_material_chars"`
		WindowGapMinutes  int    `yaml:"window_gap_minutes"`
		WindowMaxMessages int    `yaml:"window_max_messages"`
	} `yaml:"factengine"`
	Proactive struct {
		Enabled             bool   `yaml:"enabled"`
		Schedule            string `yaml:"schedule"`
		StartupDelaySeconds int    `yaml:"startup_delay_seconds"`
		Bin                 string `yaml:"bin"`
		Model               string `yaml:"model"`
		Sandbox             string `yaml:"sandbox"`
		ReasoningEffort     string `yaml:"reasoning_effort"`
		TimeoutSeconds      int    `yaml:"timeout_seconds"`
	} `yaml:"proactive"`
	LarkCLI struct {
		RateLimit  float64 `yaml:"rate_limit"`
		Burst      int     `yaml:"burst"`
		Concurrent int     `yaml:"concurrent"`
		TimeoutSec int     `yaml:"timeout_sec"`
	} `yaml:"lark_cli"`
	ScheduledTask struct {
		Enabled    bool   `yaml:"enabled"`
		Schedule   string `yaml:"schedule"`
		BatchLimit int    `yaml:"batch_limit"`
	} `yaml:"scheduled_task"`
	DailyDigest struct {
		Enabled           bool   `yaml:"enabled"`
		Schedule          string `yaml:"schedule"`
		GroupMessageLimit int    `yaml:"group_message_limit"`
		GroupConcurrency  int    `yaml:"group_concurrency"`
	} `yaml:"dailydigest"`
}

func runtimeOverrideFromSettings(input RuntimeSettings) runtimeOverride {
	var override runtimeOverride
	override.Identity.DisplayName = strings.TrimSpace(input.AgentDisplayName)
	override.Extract.Enabled = input.ExtractEnabled
	override.Extract.Engine = strings.TrimSpace(input.ExtractEngine)
	override.Extract.Schedule = strings.TrimSpace(input.ExtractSchedule)
	override.Extract.Concurrency = input.ExtractConcurrency
	override.Extract.BatchMessages = input.ExtractBatchMessages
	override.Extract.Sandbox = strings.TrimSpace(input.ExtractSandbox)
	override.Extract.Network = input.ExtractNetworkEnabled
	override.Extract.ReasoningEffort = strings.TrimSpace(input.ExtractReasoningEffort)
	override.Extract.ContextMessages = input.ExtractContextMessages
	override.Extract.ContextWindowMinutes = input.ExtractContextWindowMinutes
	override.Extract.MaxPromptChars = input.ExtractMaxPromptChars
	override.Extract.SemanticThreshold = input.ExtractSemanticThreshold
	override.Extract.SemanticNeighborLimit = input.ExtractSemanticNeighborLimit
	override.Extract.ToolTimeoutSec = input.ExtractToolTimeoutSeconds
	override.Extract.HistoryToolLimit = input.ExtractHistoryToolLimit
	override.Extract.EvidenceRetryMax = input.ExtractEvidenceRetryMax
	override.Model.Model = strings.TrimSpace(input.ModelAPIModel)
	override.Model.TimeoutSec = input.ModelAPITimeoutSeconds
	override.Codex.Bin = strings.TrimSpace(input.AnalysisCLI)
	override.Codex.Model = strings.TrimSpace(input.AnalysisModel)
	override.Codex.TimeoutSeconds = input.AnalysisTimeoutSeconds
	override.Execute.Enabled = input.ExecuteAutoEnabled
	override.Execute.Bin = strings.TrimSpace(input.ExecuteCLI)
	override.Execute.Model = strings.TrimSpace(input.ExecuteModel)
	override.Execute.ReasoningEffort = strings.TrimSpace(input.ExecuteReasoningEffort)
	override.Execute.Schedule = strings.TrimSpace(input.ExecuteSchedule)
	override.Execute.BatchLimit = input.ExecuteBatchLimit
	override.Execute.TimeoutSecond = input.ExecuteTimeoutSeconds
	override.Execute.StaleExecutingMinute = input.ExecuteStaleMinutes
	override.Execute.Concurrency = input.ExecuteConcurrency
	override.Chat.Model = strings.TrimSpace(input.ChatModel)
	override.Chat.Sandbox = strings.TrimSpace(input.ChatSandbox)
	override.Chat.ReasoningEffort = strings.TrimSpace(input.ChatReasoningEffort)
	override.Chat.TimeoutSeconds = input.ChatTimeoutSeconds
	override.Capture.PageSize = input.CapturePageSize
	override.Capture.ScanWorkers = input.CaptureScanWorkers
	override.Capture.DiscoverSchedule = strings.TrimSpace(input.CaptureDiscoverSchedule)
	override.Capture.ScanSchedule = strings.TrimSpace(input.CaptureScanSchedule)
	override.Capture.P2PActivationWindowMinutes = input.CaptureP2PWindowMinutes
	override.FactEngine.Enabled = input.FactEngineEnabled
	override.FactEngine.Schedule = strings.TrimSpace(input.FactEngineSchedule)
	override.FactEngine.Model = strings.TrimSpace(input.FactEngineModel)
	override.FactEngine.ReasoningEffort = strings.TrimSpace(input.FactEngineReasoningEffort)
	override.FactEngine.TimeoutSec = input.FactEngineTimeoutSeconds
	override.FactEngine.BatchLimit = input.FactEngineBatchLimit
	override.FactEngine.MaxMaterialChars = input.FactEngineMaxMaterialChars
	override.FactEngine.WindowGapMinutes = input.FactEngineWindowGapMinutes
	override.FactEngine.WindowMaxMessages = input.FactEngineWindowMaxMessages
	override.Proactive.Enabled = input.ProactiveEnabled
	override.Proactive.Schedule = strings.TrimSpace(input.ProactiveSchedule)
	override.Proactive.StartupDelaySeconds = input.ProactiveStartupDelaySeconds
	override.Proactive.Bin = strings.TrimSpace(input.ProactiveCLI)
	override.Proactive.Model = strings.TrimSpace(input.ProactiveModel)
	override.Proactive.Sandbox = strings.TrimSpace(input.ProactiveSandbox)
	override.Proactive.ReasoningEffort = strings.TrimSpace(input.ProactiveReasoningEffort)
	override.Proactive.TimeoutSeconds = input.ProactiveTimeoutSeconds
	override.LarkCLI.RateLimit = input.LarkRateLimit
	override.LarkCLI.Burst = input.LarkBurst
	override.LarkCLI.Concurrent = input.LarkConcurrency
	override.LarkCLI.TimeoutSec = input.LarkTimeoutSeconds
	override.ScheduledTask.Enabled = input.ScheduledTaskEnabled
	override.ScheduledTask.Schedule = strings.TrimSpace(input.ScheduledTaskSchedule)
	override.ScheduledTask.BatchLimit = input.ScheduledTaskBatchLimit
	override.DailyDigest.Enabled = input.DailyDigestEnabled
	override.DailyDigest.Schedule = strings.TrimSpace(input.DailyDigestSchedule)
	override.DailyDigest.GroupMessageLimit = input.DailyDigestMessageLimit
	override.DailyDigest.GroupConcurrency = input.DailyDigestConcurrency
	return override
}

func UpdateRuntimeOverride(configPath string, patch any) error {
	path := RuntimeOverridePath(configPath)
	document, err := readRuntimeOverrideDocument(path)
	if err != nil {
		return err
	}
	var updates yaml.Node
	if err := updates.Encode(patch); err != nil {
		return fmt.Errorf("encode runtime config update: %w", err)
	}
	mergeRuntimeMapping(document.Content[0], &updates)
	// 这次写回是覆盖文件自愈的唯一时机：合并是在现有 YAML 文档上做的，不剪掉
	// 升级残留的未知键，它们会被原样保留、每次启动都再告警一次。
	pruneUnknownRuntimeOverrideKeys(document.Content[0])
	data, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("marshal runtime config override: %w", err)
	}
	base, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if err := rejectRuntimeOverrideBaseOnlySections(data, path, configPath); err != nil {
		return err
	}
	if err := validateMergedConfig(base, data); err != nil {
		return err
	}
	return writeRuntimeOverrideBytes(path, data)
}

func mergeRuntimeMapping(target, patch *yaml.Node) {
	for i := 0; i+1 < len(patch.Content); i += 2 {
		key, value := patch.Content[i], patch.Content[i+1]
		existing := mappingValue(target, key.Value)
		if existing == nil {
			target.Content = append(target.Content, key, value)
		} else if existing.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
			mergeRuntimeMapping(existing, value)
		} else {
			value.HeadComment, value.LineComment, value.FootComment = existing.HeadComment, existing.LineComment, existing.FootComment
			*existing = *value
		}
	}
}
