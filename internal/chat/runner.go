// Package chat 提供基于 Agent CLI 的流式对话服务。
//
// 与 internal/execute 的一次性 Agent 封装不同，本包用 StdoutPipe + json.Decoder
// 边读 CLI 的 JSONL 事件边通过回调吐出，支撑 /api/chat 的 SSE 流式对话。
// 本地可信环境：CLI 跑 danger-full-access + 联网，能调用 jarvis-tools、
// 调用 jarvis-tools/lark-cli/git。fail-fast：非零退出、超时、JSON 解析失败都
// 转成 error 事件并返回 error，绝不静默吞。
package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// EventKind 是 runner 向上游吐出的流式事件类型。它与 SSE 契约一一对应，
// 但刻意与 HTTP/SSE 解耦——runner 只关心 codex，不认识 Hertz。
type EventKind string

const (
	// EventThread 携带 Agent CLI 的 thread_id（会话建立/恢复），尽早发一次。
	EventThread EventKind = "thread"
	// EventDelta 携带 codex 的增量文本，逐条发。
	EventDelta EventKind = "delta"
)

// Event 是一条 runner 流式事件。done/error 不走此结构：done 由 Stream 正常返回
// nil 表达，error 由 Stream 返回 error 表达，交给 handler 统一转成 SSE。
type Event struct {
	Kind EventKind
	// ThreadID 仅在 Kind==EventThread 时有值。
	ThreadID string
	// Text 仅在 Kind==EventDelta 时有值。
	Text string
}

// runner 封装 Codex 兼容 CLI 或 Cursor CLI 的流式调用。它持有已解析的 bin 与固定的模型/沙箱/
// reasoning_effort/超时，Service 组装好 prompt 后交给它执行。
type runner struct {
	bin             string
	provider        string
	model           string
	fastMode        bool
	sandbox         string
	reasoningEffort string
	timeout         time.Duration
}

const (
	providerCodex      = "codex"
	providerCursor     = "cursor"
	cursorThreadPrefix = "cursor_"
)

var errUnresumableThread = errors.New("chat thread belongs to another CLI")

func newRunner(bin, model, sandbox, reasoningEffort string, fastMode bool, timeout time.Duration) (*runner, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, fmt.Errorf("chat CLI bin is required")
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("find chat CLI binary %q: %w", bin, err)
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("chat CLI model is required")
	}
	switch sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("chat CLI sandbox must be read-only, workspace-write or danger-full-access, got %q", sandbox)
	}
	switch reasoningEffort {
	case "minimal", "low", "medium", "high", "xhigh":
	default:
		return nil, fmt.Errorf("chat CLI reasoning_effort must be minimal/low/medium/high/xhigh, got %q", reasoningEffort)
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("chat CLI timeout must be positive")
	}
	provider := providerCodex
	if filepath.Base(resolved) == "cursor-agent" {
		provider = providerCursor
		if fastMode {
			return nil, fmt.Errorf("chat CLI Fast Mode is not supported by cursor-agent")
		}
		if sandbox != "danger-full-access" {
			return nil, fmt.Errorf("Cursor chat currently requires danger-full-access sandbox")
		}
	}
	return &runner{
		bin:             resolved,
		provider:        provider,
		model:           model,
		fastMode:        fastMode,
		sandbox:         sandbox,
		reasoningEffort: reasoningEffort,
		timeout:         timeout,
	}, nil
}

// args 构造 codex 命令行。resume 子命令与首轮的可用 flag 不同：
//   - 首轮：codex exec --json --color never --sandbox <s> -c ... --model <m> -
//   - 多轮：codex exec resume <thread_id> --json -c sandbox_mode="<s>" -c ... --model <m> -
//
// 事实来自实跑 codex（见包测试样本）：resume 不接受 --color/--sandbox flag，
// 沙箱只能经 -c sandbox_mode 覆盖，否则 codex 直接以 exit 2 报 unexpected argument。
func (r *runner) args(threadID, imagePath string) []string {
	if r.provider == providerCursor {
		args := []string{
			"-p", "--output-format", "stream-json", "--stream-partial-output",
			"--model", r.model, "--force", "--sandbox", "disabled", "--approve-mcps", "--trust",
		}
		if threadID != "" {
			args = append(args, "--resume", strings.TrimPrefix(threadID, cursorThreadPrefix))
		}
		return args
	}
	fastArgs := []string{}
	if r.fastMode {
		fastArgs = []string{"-c", "features.fast_mode=true", "-c", `service_tier="fast"`}
	}
	var args []string
	if strings.TrimSpace(threadID) == "" {
		args = []string{
			"exec", "--json", "--color", "never",
			"--sandbox", r.sandbox,
			"-c", "sandbox_workspace_write.network_access=true",
			"--skip-git-repo-check",
			"-c", "model_reasoning_effort=" + r.reasoningEffort,
			"--model", r.model,
		}
		args = append(args, fastArgs...)
		if strings.TrimSpace(imagePath) != "" {
			args = append(args, "--image", imagePath)
		}
		args = append(args, "-")
		return args
	}
	args = []string{
		"exec", "resume", threadID, "--json",
		"-c", fmt.Sprintf("sandbox_mode=%q", r.sandbox),
		"-c", "sandbox_workspace_write.network_access=true",
		"--skip-git-repo-check",
		"-c", "model_reasoning_effort=" + r.reasoningEffort,
		"--model", r.model,
	}
	args = append(args, fastArgs...)
	if strings.TrimSpace(imagePath) != "" {
		args = append(args, "--image", imagePath)
	}
	args = append(args, "-")
	return args
}

// Stream 执行一轮 Agent CLI 对话。prompt 从 stdin 灌入；threadID 非空则 resume。
// 每解析出一条 thread/delta 事件就回调 emit；emit 返回 error（如 SSE 写失败）
// 会中止本轮并杀掉子进程。正常结束返回 nil（上游据此发 done）；任何异常
// （非零退出、超时、JSON 解析失败、stderr 有内容而无输出）返回 error。
func (r *runner) Stream(ctx context.Context, prompt, threadID, imagePath string, emit func(Event) error) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("chat prompt is required")
	}

	if threadID != "" {
		isCursorThread := strings.HasPrefix(threadID, cursorThreadPrefix)
		if (r.provider == providerCursor) != isCursorThread {
			return errUnresumableThread
		}
	}
	if r.provider == providerCursor && strings.TrimSpace(imagePath) != "" {
		prompt = fmt.Sprintf("%s\n\n本轮附带了一张图片。请先使用图片读取工具查看这个本地文件，再回答：%s", prompt, imagePath)
	}

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	command := exec.CommandContext(runCtx, r.bin, r.args(threadID, imagePath)...)
	// 取消时先发 SIGTERM 而不是默认的 SIGKILL：CLI 要收到信号才会释放 thread-store
	// 的写入者占用，被 SIGKILL 打死可能留下占用，让这个 thread 之后都 resume 不了。
	// WaitDelay 只兜住 Wait 里的等待；下面的 parseCodexStream 排在 Wait 之前，真遇到
	// 孙子进程攥着 stdout 不放仍会卡在读上。等待接管的下一轮由 Service 侧的
	// threadTakeoverTimeout 兜底，不指望这里。
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = 5 * time.Second
	command.Env = append(os.Environ(), "JARVIS_AGENT_STAGE=chat")
	command.Stdin = strings.NewReader(prompt)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open chat CLI stdout pipe: %w", err)
	}
	// stderr 单独收集：codex 的错误细节都在这里，用于失败时拼进 error。
	var stderr stderrCollector
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return fmt.Errorf("start chat CLI: %w", err)
	}

	// 边读边解析 codex JSONL。parseErr 记录解析/回调阶段的第一个错误；
	// 无论如何都要 Wait 回收子进程，避免僵尸与句柄泄漏。
	var parseErr error
	if r.provider == providerCursor {
		parseErr = parseCursorStream(stdout, emit)
	} else {
		parseErr = parseCodexStream(stdout, emit)
	}
	waitErr := command.Wait()

	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("chat CLI timed out after %s: %s", r.timeout, stderr.text())
	}
	if waitErr != nil {
		return fmt.Errorf("chat CLI exited abnormally: %w: %s", waitErr, stderr.text())
	}
	if parseErr != nil {
		return parseErr
	}
	return nil
}

// isUnresumableThread reports that the stored Agent session cannot be continued
// by the current CLI. Official Codex says this when the thread ID belongs to
// another engine (for example a previous TraeX session) or the rollout file is gone.
func isUnresumableThread(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errUnresumableThread) || strings.Contains(err.Error(), "no rollout found for thread id")
}

// parseCursorStream maps Cursor's stream-json protocol onto the stable
// thread/delta events consumed by the chat service. With partial streaming,
// Cursor emits timestamped text chunks followed by one untimestamped complete
// assistant message; only the chunks are forwarded to avoid duplicate text.
func parseCursorStream(stdout io.Reader, emit func(Event) error) error {
	decoder := json.NewDecoder(bufio.NewReader(stdout))
	sawThread := false
	sawPartial := false
	for {
		var event cursorEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode Cursor JSONL stream: %w", err)
		}
		switch event.Type {
		case "system":
			if event.Subtype != "init" {
				continue
			}
			if strings.TrimSpace(event.SessionID) == "" {
				return fmt.Errorf("Cursor init event is missing session_id")
			}
			sawThread = true
			if err := emit(Event{Kind: EventThread, ThreadID: cursorThreadPrefix + event.SessionID}); err != nil {
				return err
			}
		case "assistant":
			text := event.assistantText()
			if text == "" {
				continue
			}
			if event.TimestampMS != nil {
				sawPartial = true
			} else if sawPartial {
				continue
			}
			if err := emit(Event{Kind: EventDelta, Text: text}); err != nil {
				return err
			}
		case "result":
			if event.IsError {
				return fmt.Errorf("Cursor agent failed: %s", strings.TrimSpace(event.Result))
			}
		}
	}
	if !sawThread {
		return fmt.Errorf("Cursor JSONL output is missing system init event")
	}
	return nil
}

type cursorEvent struct {
	Type        string         `json:"type"`
	Subtype     string         `json:"subtype"`
	SessionID   string         `json:"session_id"`
	TimestampMS *int64         `json:"timestamp_ms"`
	Message     *cursorMessage `json:"message"`
	IsError     bool           `json:"is_error"`
	Result      string         `json:"result"`
}

type cursorMessage struct {
	Role    string          `json:"role"`
	Content []cursorContent `json:"content"`
}

type cursorContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (e cursorEvent) assistantText() string {
	if e.Message == nil || e.Message.Role != "assistant" {
		return ""
	}
	var b strings.Builder
	for _, content := range e.Message.Content {
		if content.Type == "text" {
			b.WriteString(content.Text)
		}
	}
	return b.String()
}

// parseCodexStream 逐事件解析 codex 的 JSONL stdout，把 thread/delta 通过 emit 吐出。
//
// 用 json.Decoder 而非行扫描：单条 JSONL 事件（如命令捕获的输出）可能超过任何
// 固定行缓冲。decoder 按 value 读，不受行长限制。
//
// 真实事件结构（实跑 codex gpt-5.5 --json 得到，不臆测字段名）：
//
//	{"type":"thread.started","thread_id":"..."}
//	{"type":"turn.started"}
//	{"type":"item.delta","delta":"..."}                      // 流式增量（部分构建才有）
//	{"type":"item.completed","item":{"type":"agent_message","text":"..."}}
//	{"type":"turn.completed","usage":{...}}
//
// 当前 codex 构建对 agent_message 只发 item.completed（整段 text），不发 item.delta；
// 但为兼容开启流式增量的构建，两者都解析：delta 逐条吐，completed 的 agent_message
// 作为一整条 delta 吐（避免重复：completed 与 delta 二选一由 codex 决定）。
func parseCodexStream(stdout io.Reader, emit func(Event) error) error {
	decoder := json.NewDecoder(bufio.NewReader(stdout))
	sawThread := false
	for {
		var event codexEvent
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("decode codex JSONL stream: %w", err)
		}
		switch event.Type {
		case "thread.started":
			if strings.TrimSpace(event.ThreadID) == "" {
				return fmt.Errorf("codex thread.started event is missing thread_id")
			}
			sawThread = true
			if err := emit(Event{Kind: EventThread, ThreadID: event.ThreadID}); err != nil {
				return err
			}
		case "item.delta":
			// 流式增量事件（若 codex 构建启用）。空 delta 跳过，不发无意义事件。
			if event.Delta == "" {
				continue
			}
			if err := emit(Event{Kind: EventDelta, Text: event.Delta}); err != nil {
				return err
			}
		case "item.completed":
			// 只关心 agent_message；工具调用/命令等其它 item 类型不进对话流。
			if event.Item == nil || event.Item.Type != "agent_message" {
				continue
			}
			if event.Item.Text == "" {
				continue
			}
			if err := emit(Event{Kind: EventDelta, Text: event.Item.Text}); err != nil {
				return err
			}
		}
	}
	if !sawThread {
		return fmt.Errorf("codex JSONL output is missing thread.started event")
	}
	return nil
}

// codexEvent 是 codex --json 输出的一条 JSONL 事件的解析目标。只声明关心的字段。
type codexEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Delta    string          `json:"delta"`
	Item     *codexEventItem `json:"item"`
}

type codexEventItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// stderrCollector 是一个受限缓冲的 io.Writer，收集 codex stderr 的前若干字节
// 用于失败诊断，避免无界内存增长。并发安全（Wait 与读循环可能并行触达）。
type stderrCollector struct {
	mu  sync.Mutex
	buf []byte
}

const maxStderrBytes = 8192

func (s *stderrCollector) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if room := maxStderrBytes - len(s.buf); room > 0 {
		if len(p) <= room {
			s.buf = append(s.buf, p...)
		} else {
			s.buf = append(s.buf, p[:room]...)
		}
	}
	return len(p), nil
}

func (s *stderrCollector) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := strings.TrimSpace(string(s.buf))
	if t == "" {
		return "(no stderr)"
	}
	return t
}
