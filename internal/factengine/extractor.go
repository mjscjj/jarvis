package factengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxExtractorOutputBytes = 1 << 20

// ExtractorOptions configures the agent CLI that distils facts. There is no
// reasoning-effort setting: the engine runs a cheap fast model where the flag is
// not known to apply, and passing an unsupported option would fail every call.
type ExtractorOptions struct {
	Bin     string
	Model   string
	Sandbox string
	Timeout time.Duration
}

// Extractor runs the agent CLI once per source unit. The CLI is a full agent: it
// can run jarvis-tools itself to check which facts a subject already has, so
// there is no Go-side tool loop here.
type Extractor struct {
	bin     string
	model   string
	sandbox string
	timeout time.Duration
}

func NewExtractor(opts ExtractorOptions) (*Extractor, error) {
	if strings.TrimSpace(opts.Bin) == "" {
		return nil, fmt.Errorf("fact extractor bin is required")
	}
	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("find fact extractor binary %q: %w", opts.Bin, err)
	}
	if strings.TrimSpace(opts.Model) == "" {
		return nil, fmt.Errorf("fact extractor model is required")
	}
	switch opts.Sandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("fact extractor sandbox %q is invalid", opts.Sandbox)
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("fact extractor timeout must be positive")
	}
	return &Extractor{bin: bin, model: opts.Model, sandbox: opts.Sandbox, timeout: opts.Timeout}, nil
}

// ExtractedFact is one fact the model decided to keep. Only the three fields the
// fact table needs are read; anything else the model chooses to say alongside
// them is ignored rather than rejected, so the protocol can grow in the prompt
// without a code change.
type ExtractedFact struct {
	SubjectType string `json:"subject_type"`
	SubjectID   uint64 `json:"subject_id"`
	Description string `json:"description"`
}

// extractionResult is the response envelope. Facts is a pointer so an empty
// array ("nothing here worth keeping", the common and correct answer) is
// distinguishable from a missing key (a malformed response).
type extractionResult struct {
	Facts *[]ExtractedFact `json:"facts"`
}

// decodeRetryMax is how many extra attempts one unit gets when the model's
// answer is not parseable JSON. The fast model this engine runs on gets the
// facts right and the envelope wrong often enough — prose before the object, an
// ASCII quote inside a description — that failing the round outright would park
// the whole source behind one badly formatted answer. The retry shows the model
// its own parse error; a second failure aborts loudly.
const decodeRetryMax = 1

// Extract distils one unit. An empty slice is a valid, expected result: most
// conversations contain nothing worth remembering for weeks.
func (e *Extractor) Extract(ctx context.Context, systemPrompt string, unit SourceUnit) ([]ExtractedFact, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return nil, fmt.Errorf("fact extraction system prompt is empty")
	}
	userPrompt, err := unit.Prompt()
	if err != nil {
		return nil, err
	}
	prompt := systemPrompt + "\n\n" + userPrompt
	for attempt := 0; ; attempt++ {
		raw, err := e.run(ctx, unit, prompt)
		if err != nil {
			return nil, err
		}
		facts, decodeErr := DecodeFacts(raw)
		if decodeErr == nil {
			return facts, nil
		}
		if attempt >= decodeRetryMax {
			return nil, fmt.Errorf("decode fact extraction unit=%s: %w", unit.Key, decodeErr)
		}
		prompt = systemPrompt + "\n\n" + userPrompt + "\n\n" + decodeFeedback(decodeErr)
	}
}

// decodeFeedback tells the model what was wrong with its envelope. The facts it
// found were probably fine; only the formatting has to change.
func decodeFeedback(decodeErr error) string {
	return "【上一轮输出无法解析，请重新输出】\n" +
		"解析报错：" + decodeErr.Error() + "\n\n" +
		"你上一轮判断的事实内容可能是对的，问题出在格式。请重新输出同样的结论，严格满足：\n" +
		"1. 整个回复只有一个 JSON 对象，前后不要有任何说明文字，也不要包在代码块里。\n" +
		"2. description 正文里不要出现英文双引号，需要引用就用「」。\n" +
		"3. 正文里不要换行。\n" +
		"4. 顶层必须有 facts 数组；确实没有值得记的事实就输出 {\"facts\": []}。"
}

func (e *Extractor) run(ctx context.Context, unit SourceUnit, prompt string) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "jarvis-fact-extract-")
	if err != nil {
		return nil, fmt.Errorf("create fact extraction temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	resultPath := filepath.Join(tempDir, "facts.json")

	// No --output-schema: the fast model this engine runs on cannot satisfy the
	// CLI's structured-output step and answers "{}" for every unit, discarding
	// facts it had already reasoned its way to. Stating the contract in the prompt
	// and checking it in DecodeFacts gets correct JSON out of the same model for
	// roughly a third of the tokens.
	args := []string{
		"exec", "--ephemeral", "--sandbox", e.sandbox, "--color", "never",
		"--output-last-message", resultPath,
		"--model", e.model, "--skip-git-repo-check", "-",
	}
	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, e.bin, args...)
	command.Dir = tempDir
	command.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("fact extraction unit=%s timed out after %s", unit.Key, e.timeout)
		}
		return nil, fmt.Errorf("fact extraction unit=%s failed: %w: %s", unit.Key, err, limitedText(stderr.Bytes(), 4096))
	}
	return readLimitedFile(resultPath, maxExtractorOutputBytes)
}

// DecodeFacts validates one extraction response. Nothing enforces the shape at
// the model boundary, so this is where the contract stated in the prompt is
// actually held: a response that does not carry a facts array fails the round
// rather than being read as "no facts here".
func DecodeFacts(raw []byte) ([]ExtractedFact, error) {
	trimmed := extractJSONObject(stripCodeFence(bytes.TrimSpace(raw)))
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("response is empty")
	}
	var result extractionResult
	if err := json.Unmarshal(trimmed, &result); err != nil {
		return nil, fmt.Errorf("parse response %s: %w", limitedText(trimmed, 512), err)
	}
	if result.Facts == nil {
		return nil, fmt.Errorf("response has no facts key: %s", limitedText(trimmed, 512))
	}
	facts := *result.Facts
	for i := range facts {
		facts[i].SubjectType = strings.TrimSpace(facts[i].SubjectType)
		facts[i].Description = strings.TrimSpace(facts[i].Description)
		if facts[i].SubjectType == "" || facts[i].SubjectID == 0 || facts[i].Description == "" {
			return nil, fmt.Errorf("fact %d needs subject_type, a positive subject_id and description, got %+v", i+1, facts[i])
		}
	}
	return facts, nil
}

// extractJSONObject takes the outermost {...} out of a response that opens with
// a sentence of narration. Only the surrounding prose is discarded; whatever is
// between the braces still has to parse and satisfy the contract.
func extractJSONObject(raw []byte) []byte {
	if bytes.HasPrefix(raw, []byte("{")) {
		return raw
	}
	start := bytes.IndexByte(raw, '{')
	end := bytes.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return raw
	}
	return raw[start : end+1]
}

// stripCodeFence unwraps a ```json block. The prompt asks for bare JSON, but
// wrapping it is a habit strong enough in chat models that tolerating the fence
// is cheaper than losing a round to it. Only the wrapper is removed; malformed
// JSON inside still fails.
func stripCodeFence(raw []byte) []byte {
	if !bytes.HasPrefix(raw, []byte("```")) {
		return raw
	}
	body := raw[3:]
	if newline := bytes.IndexByte(body, '\n'); newline >= 0 {
		body = body[newline+1:]
	}
	if end := bytes.LastIndex(body, []byte("```")); end >= 0 {
		body = body[:end]
	}
	return bytes.TrimSpace(body)
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open fact extraction result: %w", err)
	}
	defer file.Close()
	result, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read fact extraction result: %w", err)
	}
	if int64(len(result)) > limit {
		return nil, fmt.Errorf("fact extraction result exceeds %d bytes", limit)
	}
	return result, nil
}

func limitedText(b []byte, limit int) string {
	b = bytes.TrimSpace(b)
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "...(truncated)"
}
