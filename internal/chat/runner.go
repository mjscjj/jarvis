// Package chat provides persistent streaming conversations backed by the
// installed Codex, TRAE, or Cursor CLI.
//
// 与 internal/execute 的任务执行封装不同，本包用 StdoutPipe + json.Decoder
// 边读原生事件边通过回调吐出，支撑 /api/chat/* 的 SSE 流式对话。
// 本地可信环境：codex 跑 danger-full-access + 联网，能调用 jarvis-tools、
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

// processWaitDelay 是子进程退出（或被取消）之后，允许 Wait 继续等待管道关闭的
// 上限。只在异常路径生效，正常一轮对话读完即退出。
const processWaitDelay = 5 * time.Second

// EventKind 是 runner 向上游吐出的流式事件类型。它与 SSE 契约一一对应，
// 但刻意与 HTTP/SSE 解耦——runner 只关心 codex，不认识 Hertz。
type EventKind string

const (
	// EventAccepted confirms that StreamSession has persisted the user's input.
	EventAccepted EventKind = "accepted"
	// EventThread 携带 codex 的 thread_id（会话建立/恢复），尽早发一次。
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

// runner 封装 codex CLI 的流式调用。它持有已解析的 bin 与固定的模型/沙箱/
// reasoning_effort/超时，Service 组装好 prompt 后交给它执行。
type runner struct {
	commandArgs     []string
	agent           string
	bin             string
	model           string
	sandbox         string
	reasoningEffort string
	timeout         time.Duration
}

func newRunner(bin, model, sandbox, reasoningEffort string, timeout time.Duration) (*runner, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, fmt.Errorf("chat codex bin is required")
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("find chat codex binary %q: %w", bin, err)
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("chat codex model is required")
	}
	switch sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("chat codex sandbox must be read-only, workspace-write or danger-full-access, got %q", sandbox)
	}
	if strings.TrimSpace(reasoningEffort) == "" {
		return nil, fmt.Errorf("chat reasoning_effort is required")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("chat codex timeout must be positive")
	}
	return &runner{
		agent:           agentFromBinary(resolved),
		bin:             resolved,
		model:           model,
		sandbox:         sandbox,
		reasoningEffort: reasoningEffort,
		timeout:         timeout,
	}, nil
}

func agentFromBinary(bin string) string {
	base := strings.ToLower(filepath.Base(bin))
	if strings.Contains(base, "cursor") || base == "agent" {
		return "cursor"
	}
	if strings.Contains(base, "trae") {
		return "trae"
	}
	return "codex"
}

func newAgentRunner(ctx context.Context, agent, model, sandbox, reasoningEffort string, timeout time.Duration) (*runner, error) {
	agent, err := normalizeAgent(agent)
	if err != nil {
		return nil, err
	}
	bin, _, err := resolveAgentExecutable(ctx, agent)
	if err != nil {
		return nil, err
	}
	r, err := newRunner(bin, model, sandbox, reasoningEffort, timeout)
	if err != nil {
		return nil, err
	}
	r.agent = agent
	return r, nil
}

// args 构造 codex 命令行。resume 子命令与首轮的可用 flag 不同：
//   - 首轮：codex exec --json --color never --sandbox <s> -c ... --model <m> -
//   - 多轮：codex exec resume <thread_id> --json -c sandbox_mode="<s>" -c ... --model <m> -
//
// 事实来自实跑 codex（见包测试样本）：resume 不接受 --color/--sandbox flag，
// 沙箱只能经 -c sandbox_mode 覆盖，否则 codex 直接以 exit 2 报 unexpected argument。
func (r *runner) args(threadID string, imagePaths []string) []string {
	if r.agent == "cursor" {
		args := []string{"--print", "--output-format", "stream-json", "--stream-partial-output", "--force", "--model", r.model}
		if strings.TrimSpace(threadID) != "" {
			args = append(args, "--resume", strings.TrimSpace(threadID))
		}
		// Cursor reads stdin only when the positional prompt is omitted. Passing
		// "-" sends a literal dash as the prompt instead.
		return args
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
			"-",
		}
		return appendImageArgs(args, imagePaths)
	}
	args = []string{
		"exec", "resume", threadID, "--json",
		"-c", fmt.Sprintf("sandbox_mode=%q", r.sandbox),
		"-c", "sandbox_workspace_write.network_access=true",
		"--skip-git-repo-check",
		"-c", "model_reasoning_effort=" + r.reasoningEffort,
		"--model", r.model,
		"-",
	}
	return appendImageArgs(args, imagePaths)
}

func appendImageArgs(args, paths []string) []string {
	if len(paths) == 0 {
		return args
	}
	last := args[len(args)-1]
	args = args[:len(args)-1]
	for _, path := range paths {
		args = append(args, "--image", path)
	}
	return append(args, last)
}

// Stream 执行一轮 codex 对话。prompt 从 stdin 灌入；threadID 非空则 resume。
// 每解析出一条 thread/delta 事件就回调 emit；emit 返回 error（如 SSE 写失败）
// 会中止本轮并杀掉子进程。正常结束返回 nil（上游据此发 done）；任何异常
// （非零退出、超时、JSON 解析失败、stderr 有内容而无输出）返回 error。
func (r *runner) Stream(ctx context.Context, prompt, threadID string, imagePaths []string, emit func(Event) error) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("chat prompt is required")
	}

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	args := r.commandArgs
	if args == nil {
		args = r.args(threadID, imagePaths)
	}
	command := exec.CommandContext(runCtx, r.bin, args...)
	command.Env = append(os.Environ(), "JARVIS_AGENT_STAGE=chat")
	command.Stdin = strings.NewReader(prompt)
	// 自成进程组，取消时连同 CLI 派生的孙进程一起杀掉；否则 CommandContext
	// 只杀直接子进程，孙进程会继续跑。
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	// 孙进程继承 stdout/stderr 管道后，即使 CLI 本身已退出，Wait 也会一直等到
	// 管道全部关闭。没有这个上限，一个残留后台进程就能永久占住会话槽位。
	command.WaitDelay = processWaitDelay
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open codex stdout pipe: %w", err)
	}
	// stderr 单独收集：codex 的错误细节都在这里，用于失败时拼进 error。
	var stderr stderrCollector
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return fmt.Errorf("start codex: %w", err)
	}

	// 边读边解析 codex JSONL。parseErr 记录解析/回调阶段的第一个错误；
	// 无论如何都要 Wait 回收子进程，避免僵尸与句柄泄漏。
	var parseErr error
	if r.agent == "cursor" {
		parseErr = parseCursorStream(stdout, emit)
	} else {
		parseErr = parseCodexStream(stdout, emit)
	}
	// A broken downstream stream must stop the CLI before Wait. Otherwise the
	// child can block while writing to an unread stdout pipe until the turn timeout.
	if parseErr != nil {
		cancel()
	}
	waitErr := command.Wait()

	if ctx.Err() == context.Canceled {
		return context.Canceled
	}
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("codex chat timed out after %s: %s", r.timeout, stderr.text())
	}
	if parseErr != nil {
		return parseErr
	}
	if waitErr != nil {
		return fmt.Errorf("codex chat exited abnormally: %w: %s", waitErr, stderr.text())
	}
	return nil
}

// parseCursorStream accepts Cursor Agent's stream-json protocol. Cursor has
// shipped both direct text deltas and Claude-style assistant content blocks;
// the parser projects only explicit session IDs and assistant text.
func parseCursorStream(stdout io.Reader, emit func(Event) error) error {
	decoder := json.NewDecoder(bufio.NewReader(stdout))
	sawThread := false
	emitted := ""
	for {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode cursor stream JSON: %w", err)
		}
		if id := firstString(event, "session_id", "sessionId", "chat_id", "chatId"); id != "" && !sawThread {
			sawThread = true
			if err := emit(Event{Kind: EventThread, ThreadID: id}); err != nil {
				return err
			}
		}
		// Thinking events also carry a top-level text field. Only assistant
		// events belong in the visible answer.
		if firstString(event, "type") != "assistant" {
			continue
		}
		message, _ := event["message"].(map[string]any)
		content, _ := message["content"].([]any)
		_, isPartial := event["timestamp_ms"]
		for _, raw := range content {
			part, _ := raw.(map[string]any)
			if text, _ := part["text"].(string); text != "" {
				// Cursor emits a final aggregate assistant event after the partial
				// events. Project only its unseen suffix to avoid duplicate text.
				if !isPartial && strings.HasPrefix(text, emitted) {
					text = strings.TrimPrefix(text, emitted)
				}
				if text == "" {
					continue
				}
				if err := emit(Event{Kind: EventDelta, Text: text}); err != nil {
					return err
				}
				emitted += text
			}
		}
	}
	if !sawThread {
		return fmt.Errorf("cursor stream is missing session id")
	}
	return nil
}

// StreamCommand reuses the native Codex JSONL protocol for an outer execution
// adapter (for example docker run). The caller owns outer resource cleanup.
func StreamCommand(ctx context.Context, bin string, args []string, prompt string, timeout time.Duration, emit func(Event) error) error {
	r := &runner{agent: "codex", bin: bin, commandArgs: args, timeout: timeout}
	return r.Stream(ctx, prompt, "", nil, emit)
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, _ := object[key].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
