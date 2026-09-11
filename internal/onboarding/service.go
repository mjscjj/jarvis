// Package onboarding owns the desktop first-run machine boundary.
package onboarding

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"

	"gorm.io/gorm"
)

const (
	flowPending = "pending"
	flowSuccess = "success"
	flowFailed  = "failed"

	worldModelActionType = "bootstrap_world_model"
	worldModelMarkerName = "world-model.required"
)

type CommandRunner interface {
	Run(context.Context, string, []string, string) ([]byte, error)
	RunJSON(context.Context, string, []string, string) ([]byte, error)
}

type streamingCommandRunner interface {
	RunStreaming(context.Context, string, []string, string, func([]byte)) ([]byte, error)
}

type execRunner struct{}

func onboardingCommand(ctx context.Context, binary string, args []string, input string) *exec.Cmd {
	command := exec.CommandContext(ctx, binary, args...)
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}
	command.Env = append(os.Environ(),
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
		"LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1",
	)
	return command
}

func (execRunner) Run(ctx context.Context, binary string, args []string, input string) ([]byte, error) {
	return onboardingCommand(ctx, binary, args, input).CombinedOutput()
}

// JSON commands own stdout; human diagnostics on stderr are not JSON payload.
func (execRunner) RunJSON(ctx context.Context, binary string, args []string, input string) ([]byte, error) {
	command := onboardingCommand(ctx, binary, args, input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err != nil {
		if stderr.Len() > 0 {
			return stderr.Bytes(), err
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}

type synchronizedOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	onOutput func([]byte)
}

func (w *synchronizedOutput) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written, err := w.buffer.Write(value)
	if written > 0 && w.onOutput != nil {
		w.onOutput(append([]byte(nil), value[:written]...))
	}
	return written, err
}

func (w *synchronizedOutput) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buffer.Bytes()...)
}

func (execRunner) RunStreaming(
	ctx context.Context,
	binary string,
	args []string,
	input string,
	onOutput func([]byte),
) ([]byte, error) {
	command := exec.CommandContext(ctx, binary, args...)
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}
	command.Env = append(os.Environ(),
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
		"LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1",
	)
	output := &synchronizedOutput{onOutput: onOutput}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	return output.Bytes(), err
}

type Options struct {
	ConfigPath    string
	RuntimeRoot   string
	StateRoot     string
	LarkCLIBin    string
	AgentCLIBin   string
	CCConnectBin  string
	DB            *gorm.DB
	TaskSubmitter *taskcreate.Submitter
	Runner        CommandRunner
	HTTPClient    *http.Client
	Desktop       bool
}

type Service struct {
	options Options
	runner  CommandRunner

	mu                sync.Mutex
	flows             map[string]*Flow
	runtimeID         string
	runtimeConfigured bool
	bootstrapMu       sync.Mutex
}

type IdentityStatus struct {
	Status   string `json:"status"`
	OpenID   string `json:"open_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Verified bool   `json:"verified"`
}

type LarkStatus struct {
	Available           bool           `json:"available"`
	AppID               string         `json:"app_id,omitempty"`
	AppName             string         `json:"app_name,omitempty"`
	Bot                 IdentityStatus `json:"bot"`
	User                IdentityStatus `json:"user"`
	Error               string         `json:"error,omitempty"`
	CredentialAvailable bool           `json:"credential_available"`
}

type AgentStatus struct {
	Available     bool   `json:"available"`
	Authenticated bool   `json:"authenticated"`
	Error         string `json:"error,omitempty"`
}

type Status struct {
	Configuration   *config.InitializationStatus `json:"configuration"`
	Lark            LarkStatus                   `json:"lark"`
	Agent           AgentStatus                  `json:"agent"`
	AppReady        bool                         `json:"app_ready"`
	WorldModelReady bool                         `json:"world_model_ready"`
	Completed       bool                         `json:"completed"`
	AgentName       string                       `json:"agent_name"`
	RuntimeID       string                       `json:"runtime_id"`
}

type Flow struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	VerificationURL string `json:"verification_url,omitempty"`
	UserCode        string `json:"user_code,omitempty"`
	Output          string `json:"output,omitempty"`
	Error           string `json:"error,omitempty"`
	cancel          context.CancelFunc
	kind            string
}

type larkAuthPayload struct {
	AppID      string `json:"appId"`
	Verified   bool   `json:"verified"`
	Identities struct {
		Bot struct {
			Status   string `json:"status"`
			Verified bool   `json:"verified"`
			OpenID   string `json:"openId"`
			AppName  string `json:"appName"`
		} `json:"bot"`
		User struct {
			Status      string `json:"status"`
			Verified    bool   `json:"verified"`
			OpenID      string `json:"openId"`
			UserName    string `json:"userName"`
			TokenStatus string `json:"tokenStatus"`
		} `json:"user"`
	} `json:"identities"`
}

func NewService(options Options) (*Service, error) {
	if strings.TrimSpace(options.ConfigPath) == "" {
		return nil, fmt.Errorf("onboarding config path is empty")
	}
	if strings.TrimSpace(options.RuntimeRoot) == "" || strings.TrimSpace(options.StateRoot) == "" {
		return nil, fmt.Errorf("onboarding runtime paths are empty")
	}
	if options.DB == nil || options.TaskSubmitter == nil {
		return nil, fmt.Errorf("onboarding storage dependencies are nil")
	}
	if strings.TrimSpace(options.LarkCLIBin) == "" || strings.TrimSpace(options.AgentCLIBin) == "" {
		return nil, fmt.Errorf("onboarding CLI binaries are empty")
	}
	if options.Runner == nil {
		options.Runner = execRunner{}
	}
	runtimeID, err := randomID()
	if err != nil {
		return nil, err
	}
	configuration, err := config.InspectInitialization(options.ConfigPath)
	if err != nil {
		return nil, err
	}
	return &Service{options: options, runner: options.Runner, flows: make(map[string]*Flow), runtimeID: runtimeID,
		runtimeConfigured: configuration.MachineConfigurationReady}, nil
}

// Bootstrap reads only the local installation boundary. It does not claim that
// external credentials are valid; Status remains the full connection check.
func (s *Service) Bootstrap() (*BootstrapStatus, error) {
	configuration, err := config.InspectInitialization(s.options.ConfigPath)
	if err != nil {
		return nil, err
	}
	return &BootstrapStatus{MachineConfigurationReady: configuration.MachineConfigurationReady && (!s.options.Desktop || s.runtimeConfigured)}, nil
}

type BootstrapStatus struct {
	MachineConfigurationReady bool `json:"machine_configuration_ready"`
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	configuration, err := config.InspectInitialization(s.options.ConfigPath)
	if err != nil {
		return nil, err
	}
	agentName, savedOpenID, err := s.savedIdentity()
	if err != nil {
		return nil, err
	}
	// The two CLIs own independent credentials; neither check needs to wait
	// for the other. Still require both results before reporting readiness.
	agentResult := make(chan AgentStatus, 1)
	go func() { agentResult <- s.agentStatus(ctx) }()
	lark := s.larkStatus(ctx)
	agent := <-agentResult
	if s.options.Desktop && !lark.CredentialAvailable {
		configuration.MachineConfigurationReady = false
	}
	worldModelReady, err := s.worldModelReady(ctx, lark.User.OpenID)
	if err != nil {
		return nil, err
	}
	if savedOpenID != "" && savedOpenID != lark.User.OpenID {
		configuration.MachineConfigurationReady = false
		worldModelReady = false
		lark.Error = "当前飞书用户与本机已保存身份不同，请使用原账号重新授权；已有世界模型不会自动覆盖"
	}
	result := &Status{
		Configuration:   configuration,
		Lark:            lark,
		Agent:           agent,
		WorldModelReady: worldModelReady,
		AgentName:       agentName,
		RuntimeID:       s.runtimeID,
	}
	// Saving configuration does not apply it to this running desktop process.
	result.AppReady = (!s.options.Desktop || s.runtimeConfigured) && configuration.MachineConfigurationReady &&
		lark.Bot.Status == "ready" && lark.Bot.Verified && lark.User.Status == "ready" && lark.User.Verified &&
		agent.Authenticated
	result.Completed = result.AppReady && result.WorldModelReady
	return result, nil
}

func (s *Service) BeginLarkSetup(ctx context.Context) (*Flow, error) {
	current := s.larkStatus(ctx)
	if current.Error != "" {
		return nil, errors.New(current.Error)
	}
	if current.AppID != "" {
		return &Flow{Status: flowSuccess}, nil // Never create a second App.
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	for _, flow := range s.flows {
		if flow.Status == flowPending {
			if flow.kind == "lark_setup" {
				result := cloneFlow(flow)
				s.mu.Unlock()
				return result, nil
			}
			s.mu.Unlock()
			return nil, fmt.Errorf("请先完成或取消当前连接")
		}
	}
	flow := &Flow{ID: id, Status: flowPending, kind: "lark_setup"}
	s.flows[id] = flow
	result := cloneFlow(flow)
	s.mu.Unlock()
	go s.completeLarkSetup(id)
	return result, nil
}

func (s *Service) BeginLarkLogin(ctx context.Context) (*Flow, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := s.larkAuthorization(ctx, "begin")
	if err != nil {
		return nil, commandError("发起飞书授权", output, err)
	}
	values := decodeJSON(output)
	deviceCode := findString(values, "device_code", "deviceCode")
	verifyURL := findString(values, "verification_url", "verification_uri_complete", "verification_uri")
	userCode := findString(values, "user_code", "userCode")
	if deviceCode == "" || verifyURL == "" {
		return nil, fmt.Errorf("飞书授权响应缺少 device_code 或 verification_url")
	}
	flowID, err := randomID()
	if err != nil {
		return nil, err
	}
	flow := &Flow{ID: flowID, Status: flowPending, VerificationURL: verifyURL, UserCode: userCode}
	s.mu.Lock()
	s.flows[flowID] = flow
	s.mu.Unlock()
	result := cloneFlow(flow)
	go s.completeLarkLogin(flowID, deviceCode)
	return result, nil
}

func (s *Service) larkAuthorization(ctx context.Context, action string) ([]byte, error) {
	return s.runner.Run(ctx, "bash", []string{
		filepath.Join(s.options.RuntimeRoot, "scripts", "jarvis-lark-auth"), action, s.options.LarkCLIBin,
	}, "")
}

func (s *Service) BeginAgentLogin(ctx context.Context) (*Flow, error) {
	status := s.agentStatus(ctx)
	if status.Authenticated {
		return &Flow{Status: flowSuccess}, nil
	}
	flowID, err := randomID()
	if err != nil {
		return nil, err
	}
	flow := &Flow{ID: flowID, Status: flowPending}
	s.mu.Lock()
	s.flows[flowID] = flow
	s.mu.Unlock()
	result := cloneFlow(flow)
	go s.completeAgentLogin(flowID)
	return result, nil
}

func (s *Service) Flow(flowID string) (*Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow, ok := s.flows[strings.TrimSpace(flowID)]
	if !ok {
		return nil, fmt.Errorf("授权流程不存在或已过期")
	}
	return cloneFlow(flow), nil
}

func (s *Service) Finalize(ctx context.Context, agentName, gitAuthor, appSecret string) (*Status, error) {
	lark := s.larkStatus(ctx)
	if lark.Bot.Status != "ready" || !lark.Bot.Verified || lark.User.Status != "ready" || !lark.User.Verified || lark.User.OpenID == "" {
		return nil, fmt.Errorf("飞书 Bot 和用户授权必须先完成")
	}
	if !s.agentStatus(ctx).Authenticated {
		return nil, fmt.Errorf("Trae CLI 尚未登录")
	}
	appSecret = strings.TrimSpace(appSecret)
	if appSecret == "" {
		var err error
		appSecret, err = s.savedSecret(lark.AppID)
		if err != nil {
			return nil, err
		}
	}
	if err := verifyAppCredentials(ctx, s.options.HTTPClient, lark.AppID, appSecret); err != nil {
		return nil, err
	}
	if err := s.checkBotEvents(ctx); err != nil {
		return nil, err
	}
	savedName, savedOpenID, err := s.savedIdentity()
	if err != nil {
		return nil, err
	}
	if savedOpenID != "" && savedOpenID != lark.User.OpenID {
		return nil, fmt.Errorf("当前飞书用户与本机已保存身份不同。请使用原账号授权；迁移到另一位用户需单独确认世界模型处理方式，不会自动覆盖旧数据")
	}
	if strings.TrimSpace(agentName) == "" {
		agentName = savedName
		if agentName == "" {
			agentName = "Jarvis"
		}
	}
	if _, err := config.ConfigurePrincipal(
		s.options.ConfigPath,
		agentName,
		lark.User.OpenID,
		gitAuthor,
	); err != nil {
		return nil, err
	}
	if err := enableDesktopRuntime(ctx, s.options.ConfigPath); err != nil {
		return nil, err
	}
	cfg, err := config.Load(s.options.ConfigPath)
	if err != nil {
		return nil, err
	}
	if err := writeCCConfig(
		filepath.Join(s.options.StateRoot, "cc-connect", "config.toml"),
		s.options.RuntimeRoot,
		s.options.AgentCLIBin,
		lark.AppID,
		appSecret,
		lark.User.OpenID,
		cfg.CardApproval.RelaySecret,
	); err != nil {
		return nil, err
	}
	if err := s.requireWorldModel(); err != nil {
		return nil, err
	}
	// Finish slow checks before requesting restart; report write failures to the caller.
	status, err := s.Status(ctx)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(s.options.StateRoot, "restart.requested"), []byte("setup\n"), 0o600); err != nil {
		return nil, fmt.Errorf("request runtime restart: %w", err)
	}
	return status, nil
}

func (s *Service) BootstrapWorldModel(ctx context.Context) (*domain.Task, error) {
	// Browser reload/retry may overlap a previous request. Reuse one task.
	s.bootstrapMu.Lock()
	defer s.bootstrapMu.Unlock()
	lark := s.larkStatus(ctx)
	if lark.User.Status != "ready" || !lark.User.Verified || lark.User.OpenID == "" {
		return nil, fmt.Errorf("飞书用户授权必须先完成")
	}
	if err := s.requireWorldModel(); err != nil {
		return nil, err
	}
	if err := s.ensurePrincipalProfile(ctx, lark.User); err != nil {
		return nil, err
	}
	var existing domain.Task
	result := s.options.DB.WithContext(ctx).
		Where("action_type = ?", worldModelActionType).
		Order("id DESC").
		Limit(1).
		Find(&existing)
	if result.Error != nil {
		return nil, fmt.Errorf("find world model initialization task: %w", result.Error)
	}
	// Opening/reloading the app must never restart a failed task. The user
	// continues this same Task through the existing rerun/resume endpoints.
	if result.RowsAffected == 1 {
		return &existing, nil
	}
	// The original request is visible in M5's first context view. Background
	// is progressively disclosed, so it must not own the execution instruction.
	request := map[string]any{
		"source":           "desktop_onboarding",
		"requested_action": "bootstrap_world_model",
		"runtime_root":     s.options.RuntimeRoot,
		"skill_path":       filepath.Join(s.options.RuntimeRoot, ".agents", "skills", "bootstrap-jarvis-world-model", "SKILL.md"),
		"instruction":      "在 runtime_root 中工作，先完整读取并执行 skill_path 指定的初始化 Skill 及其引用。基于当前飞书用户建立本人职责、项目、关键人物、资料、重点事项、群背景与关系，按 Skill 做建立和查漏补缺两轮，并通过现有工具写入、读回。中断或重试先读取已有工作稿和任务运行记录；有 previous_task_id 时接续该任务的成果，不重复创建或清空存量。最后给出已理解的工作全景、写入位置与尚缺的信息，未完成的取证如实说明，不能冒充完成。",
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode world model initialization request: %w", err)
	}
	return s.options.TaskSubmitter.Submit(ctx, taskcreate.Input{
		Title:         "建立初始世界模型",
		ActionType:    worldModelActionType,
		Target:        "基于当前飞书身份建立 Principal、项目、人物、资料、重点事项与群监听",
		Background:    json.RawMessage(`{"onboarding":"desktop"}`),
		SourcePayload: payload,
		SourceType:    taskcreate.SourceManual,
		ActorType:     "user",
		EventDetail:   map[string]any{"channel": "desktop_onboarding"},
	})
}

func (s *Service) checkBotEvents(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	for _, event := range []string{"im.message.receive_v1", "card.action.trigger"} {
		output, err := s.runner.Run(ctx, s.options.LarkCLIBin, []string{"event", "consume", event, "--as", "bot", "--dry-run"}, "")
		if err != nil {
			return commandError("检查飞书应用事件 "+event, output, err)
		}
		var result struct {
			OK   bool `json:"ok"`
			Data struct {
				Decision struct {
					Status string `json:"status"`
				} `json:"decision"`
			} `json:"data"`
		}
		if json.Unmarshal(output, &result) != nil || !result.OK || result.Data.Decision.Status != "ready" {
			return fmt.Errorf("当前飞书应用的 %s 尚未就绪，请检查消息权限、事件订阅和版本发布后重试", event)
		}
	}
	return nil
}

func (s *Service) ensurePrincipalProfile(ctx context.Context, identity IdentityStatus) error {
	name := strings.TrimSpace(identity.Name)
	if strings.TrimSpace(identity.OpenID) == "" || name == "" {
		return fmt.Errorf("飞书用户身份缺少 open_id 或姓名")
	}
	service, err := background.NewProfileService(s.options.DB, identity.OpenID)
	if err != nil {
		return err
	}
	current, err := service.Get(ctx)
	if err != nil {
		return err
	}
	if current.Saved {
		return nil
	}
	_, err = service.Upsert(ctx, background.ProfileInput{Name: name})
	return err
}

func (s *Service) worldModelReady(ctx context.Context, principalOpenID string) (bool, error) {
	principalOpenID = strings.TrimSpace(principalOpenID)
	if principalOpenID == "" {
		return false, nil
	}
	var profileCount int64
	if err := s.options.DB.WithContext(ctx).
		Model(&domain.PrincipalProfile{}).
		Where("open_id = ?", principalOpenID).
		Count(&profileCount).Error; err != nil {
		return false, fmt.Errorf("inspect onboarding principal profile: %w", err)
	}
	if _, err := os.Stat(s.worldModelMarkerPath()); errors.Is(err, os.ErrNotExist) {
		return profileCount > 0, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect world model initialization marker: %w", err)
	}
	var task domain.Task
	result := s.options.DB.WithContext(ctx).
		Where("action_type = ?", worldModelActionType).
		Order("id DESC").
		Limit(1).
		Find(&task)
	if result.Error != nil {
		return false, fmt.Errorf("inspect world model initialization task: %w", result.Error)
	}
	return profileCount > 0 && result.RowsAffected == 1 && task.Status == "done", nil
}

func (s *Service) worldModelMarkerPath() string {
	return filepath.Join(s.options.StateRoot, worldModelMarkerName)
}

func (s *Service) requireWorldModel() error {
	if err := os.MkdirAll(s.options.StateRoot, 0o700); err != nil {
		return fmt.Errorf("create onboarding state directory: %w", err)
	}
	if err := os.WriteFile(s.worldModelMarkerPath(), []byte("required\n"), 0o600); err != nil {
		return fmt.Errorf("write world model initialization marker: %w", err)
	}
	return nil
}

func (s *Service) completeLarkLogin(flowID, deviceCode string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if !s.registerFlowCancel(flowID, cancel) {
		return
	}
	output, err := s.runner.Run(ctx, s.options.LarkCLIBin, []string{
		"auth", "login", "--device-code", deviceCode,
	}, "")
	s.finishFlow(flowID, output, err)
}

func (s *Service) completeAgentLogin(flowID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if !s.registerFlowCancel(flowID, cancel) {
		return
	}
	var output []byte
	var err error
	if runner, ok := s.runner.(streamingCommandRunner); ok {
		output, err = runner.RunStreaming(
			ctx,
			s.options.AgentCLIBin,
			[]string{"login", "--sso-device"},
			"",
			func(chunk []byte) { s.appendFlowOutput(flowID, chunk) },
		)
	} else {
		output, err = s.runner.Run(ctx, s.options.AgentCLIBin, []string{"login", "--sso-device"}, "")
	}
	s.finishFlow(flowID, output, err)
}

// lark-cli owns app creation and credential persistence. Only publish its
// browser link, never its raw config output (which may contain credentials).
func (s *Service) completeLarkSetup(flowID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if !s.registerFlowCancel(flowID, cancel) {
		return
	}
	runner, ok := s.runner.(streamingCommandRunner)
	if !ok {
		s.finishFlow(flowID, nil, fmt.Errorf("当前执行器不支持连接流程"))
		return
	}
	var output strings.Builder
	urlPattern := regexp.MustCompile(`https?://[^\s<>"\x1b]+`)
	_, err := runner.RunStreaming(ctx, s.options.LarkCLIBin, []string{"config", "init", "--new"}, "", func(chunk []byte) {
		output.Write(chunk)
		link := findString(decodeJSON([]byte(output.String())), "verification_url", "verification_uri_complete", "verification_uri")
		if link == "" {
			link = urlPattern.FindString(output.String())
		}
		if link != "" {
			s.mu.Lock()
			if flow := s.flows[flowID]; flow != nil && flow.Status == flowPending {
				flow.VerificationURL = link
			}
			s.mu.Unlock()
		}
	})
	if err != nil {
		err = fmt.Errorf("飞书连接未完成，请重试；已有授权不会被清除")
	}
	s.finishFlow(flowID, nil, err)
}

func (s *Service) appendFlowOutput(flowID string, output []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if flow, ok := s.flows[flowID]; ok {
		flow.Output += string(output)
	}
}

func (s *Service) finishFlow(flowID string, output []byte, runErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow, ok := s.flows[flowID]
	if !ok || flow.Status != flowPending {
		return
	}
	flow.Output = strings.TrimSpace(string(output))
	if runErr != nil {
		flow.Status = flowFailed
		flow.Error = commandError("完成授权", output, runErr).Error()
		return
	}
	flow.Status = flowSuccess
}

func (s *Service) registerFlowCancel(id string, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow := s.flows[id]
	if flow == nil || flow.Status != flowPending {
		return false
	}
	flow.cancel = cancel
	return true
}

func (s *Service) CancelFlow(id string) (*Flow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flow := s.flows[id]
	if flow == nil {
		return nil, fmt.Errorf("授权流程不存在或已过期")
	}
	if flow.Status == flowPending {
		flow.Status = flowFailed
		flow.Error = "已取消本次授权，可重新发起；已有授权不会被撤销"
		if flow.cancel != nil {
			flow.cancel()
		}
	}
	return cloneFlow(flow), nil
}

func (s *Service) larkStatus(ctx context.Context) LarkStatus {
	statusCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	output, err := s.runner.Run(statusCtx, s.options.LarkCLIBin, []string{"auth", "status", "--json", "--verify"}, "")
	cancel()
	if err != nil {
		var failure struct {
			Error struct {
				Type    string `json:"type"`
				Subtype string `json:"subtype"`
				Field   string `json:"field"`
			} `json:"error"`
		}
		if json.Unmarshal(output, &failure) == nil && failure.Error.Type == "config" &&
			failure.Error.Subtype == "not_configured" && failure.Error.Field == "" {
			// No app yet is a normal first-run state; an invalid selected profile is not.
			return LarkStatus{Available: true}
		}
		status := LarkStatus{Available: !errors.Is(err, exec.ErrNotFound), Error: commandError("检查飞书授权", output, err).Error()}
		// Read the configured App ID even if remote credential verification failed.
		// Keep the verification error; knowing the app is not proof it is ready.
		if ctx.Err() == nil && status.Available {
			if current, configErr := s.currentLarkConfig(ctx); configErr == nil {
				status.AppID = current.AppID
			}
		}
		return status
	}
	var payload larkAuthPayload
	if err := json.Unmarshal(output, &payload); err != nil {
		return LarkStatus{Available: true, Error: "无法解析飞书授权状态"}
	}
	secret, secretErr := s.savedSecret(payload.AppID)
	status := LarkStatus{
		Available:           true,
		AppID:               payload.AppID,
		AppName:             payload.Identities.Bot.AppName,
		CredentialAvailable: secret != "",
		Bot: IdentityStatus{
			Status: payload.Identities.Bot.Status, OpenID: payload.Identities.Bot.OpenID,
			Name: payload.Identities.Bot.AppName, Verified: payload.Identities.Bot.Verified,
		},
		User: IdentityStatus{
			Status: payload.Identities.User.Status, OpenID: payload.Identities.User.OpenID,
			Name:     payload.Identities.User.UserName,
			Verified: payload.Identities.User.Verified && payload.Identities.User.TokenStatus == "valid",
		},
	}
	if secretErr != nil {
		status.Error = secretErr.Error()
	}
	if status.User.Verified {
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if output, err := s.larkAuthorization(checkCtx, "check"); err != nil {
			status.User.Verified = false
			status.Error = commandError("检查安装所需飞书权限，请在安装页重新授权当前应用", output, err).Error()
		}
	}
	return status
}

func (s *Service) agentStatus(ctx context.Context) AgentStatus {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	loginOutput, loginErr := s.runner.Run(ctx, s.options.AgentCLIBin, []string{"login", "status"}, "")
	status := AgentStatus{Available: !errors.Is(loginErr, exec.ErrNotFound)}
	if loginErr == nil && strings.HasPrefix(strings.TrimSpace(string(loginOutput)), "Logged in") {
		status.Authenticated = true
	} else if loginErr != nil {
		status.Error = commandError("检查 Trae CLI 登录", loginOutput, loginErr).Error()
	}
	return status
}

func enableDesktopRuntime(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	service, err := config.NewRuntimeSettingsService(configPath, cfg)
	if err != nil {
		return err
	}
	view, err := service.Get(ctx)
	if err != nil {
		return err
	}
	settings := view.Settings
	settings.ExtractEnabled = true
	settings.ExecuteAutoEnabled = true
	settings.ChatEnabled = true
	settings.FactEngineEnabled = true
	settings.ProactiveEnabled = true
	settings.ScheduledTaskEnabled = true
	settings.DailyDigestEnabled = true
	_, err = service.Update(ctx, settings)
	return err
}

func writeCCConfig(path, runtimeRoot, agentBin, appID, appSecret, principalOpenID, relaySecret string) error {
	for name, value := range map[string]string{
		"App ID": appID, "App Secret": appSecret, "Principal open_id": principalOpenID,
		"relay secret": relaySecret,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is empty", name)
		}
	}
	promptPath := filepath.Join(runtimeRoot, "conf", "prompts", "cc-system-prompt.md")
	promptContent, err := os.ReadFile(promptPath)
	if err != nil {
		return fmt.Errorf("read CC system prompt %s: %w", promptPath, err)
	}
	if strings.TrimSpace(string(promptContent)) == "" {
		return fmt.Errorf("CC system prompt is empty: %s", promptPath)
	}
	prompt := strings.ReplaceAll(string(promptContent), "{{REPO_ROOT}}", runtimeRoot)
	content := fmt.Sprintf(`
data_dir = "%s"

[[projects]]
name = "jarvis-codex"
inject_sender = true

[projects.display]
mode = "quiet"
thinking_messages = false
tool_messages = false

[projects.agent]
type = "codex"

[projects.agent.options]
work_dir = "%s"
mode = "yolo"
cmd = "%s"
append_system_prompt = "%s"

[[projects.platforms]]
type = "feishu"

[projects.platforms.options]
app_id = "%s"
app_secret = "%s"
allow_from = "%s"
thread_isolation = true
document_comments = true
jarvis_approval_url = "http://127.0.0.1:18800/internal/card-approval/callback"
jarvis_approval_secret = "%s"
jarvis_approval_timeout_ms = 2500
jarvis_route_claim_url = "http://127.0.0.1:18800/internal/message-routing/claim"
jarvis_route_claim_secret = "%s"
jarvis_route_claim_timeout_ms = 2500
jarvis_event_relay_url = "http://127.0.0.1:18800/internal/meeting-sweep/wake"
jarvis_event_relay_secret = "%s"
jarvis_event_relay_types = "vc.meeting.participant_meeting_ended_v1"
`, tomlString(filepath.Join(filepath.Dir(path), "data")), tomlString(runtimeRoot), tomlString(agentBin), tomlString(prompt), tomlString(appID),
		tomlString(appSecret), tomlString(principalOpenID), tomlString(relaySecret),
		tomlString(relaySecret), tomlString(relaySecret))
	return writeSecretConfig(path, []byte(content))
}

// writeSecretConfig atomically replaces path. Owner-only because the content
// carries the Feishu app secret; atomic because a torn write leaves CC Connect
// unable to start.
func writeSecretConfig(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create CC Connect config directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func tomlString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\r", `\r`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	value = strings.ReplaceAll(value, "\t", `\t`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func decodeJSON(raw []byte) any {
	var value any
	_ = json.Unmarshal(raw, &value)
	return value
}

func findString(value any, keys ...string) string {
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	var walk func(any) string
	walk = func(current any) string {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if _, ok := wanted[key]; ok {
					if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
						return strings.TrimSpace(text)
					}
				}
			}
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return walk(value)
}

func commandError(action string, output []byte, err error) error {
	detail := strings.TrimSpace(string(bytes.TrimSpace(output)))
	if len(detail) > 1200 {
		detail = detail[:1200]
	}
	if detail == "" {
		return fmt.Errorf("%s失败: %w", action, err)
	}
	return fmt.Errorf("%s失败: %s", action, detail)
}

func randomID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func cloneFlow(flow *Flow) *Flow {
	if flow == nil {
		return nil
	}
	copy := *flow
	return &copy
}
