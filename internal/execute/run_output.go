package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	runOutputPromptFile = "prompt.txt"
	runOutputStdoutFile = "stdout.jsonl"
	runOutputStderrFile = "stderr.log"
)

type TaskRunOutput struct {
	TaskID     uint64     `json:"task_id"`
	TaskStatus string     `json:"task_status"`
	Available  bool       `json:"available"`
	Running    bool       `json:"running"`
	RunKey     string     `json:"run_key,omitempty"`
	Stage      string     `json:"stage,omitempty"`
	Prompt     string     `json:"prompt,omitempty"`
	Stdout     string     `json:"stdout,omitempty"`
	Stderr     string     `json:"stderr,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

type TaskRunOutputReader interface {
	LatestTaskRunOutput(context.Context, uint64) (*TaskRunOutput, error)
}

type taskRunOutputPaths struct {
	RunKey string
	Stage  string
	Dir    string
	Prompt string
	Stdout string
	Stderr string
}

func (e *AgentExecutor) prepareTaskRunOutput(taskID uint64, stage string, startedAt time.Time, prompt string) (*codexOutputCapture, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: task output Task ID is invalid", ErrInvalidInput)
	}
	stage = strings.TrimSpace(stage)
	if stage == "" || strings.ContainsAny(stage, `/\`) {
		return nil, fmt.Errorf("%w: invalid task output stage %q", ErrInvalidInput, stage)
	}
	taskDir := filepath.Join(e.runsDir, fmt.Sprintf("task-%d", taskID))
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return nil, fmt.Errorf("create task output directory %q: %w", taskDir, err)
	}
	runKey := fmt.Sprintf("run-%020d-%s", startedAt.UTC().UnixNano(), stage)
	finalDir := filepath.Join(taskDir, runKey)
	tempDir, err := os.MkdirTemp(taskDir, ".run-output-")
	if err != nil {
		return nil, fmt.Errorf("create temporary task output directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	for name, content := range map[string]string{
		runOutputPromptFile: prompt,
		runOutputStdoutFile: "",
		runOutputStderrFile: "",
	} {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte(content), 0o600); err != nil {
			return nil, fmt.Errorf("initialize task output %s: %w", name, err)
		}
	}
	if err := os.Rename(tempDir, finalDir); err != nil {
		return nil, fmt.Errorf("publish task output directory %q: %w", finalDir, err)
	}
	return &codexOutputCapture{
		StdoutPath: filepath.Join(finalDir, runOutputStdoutFile),
		StderrPath: filepath.Join(finalDir, runOutputStderrFile),
	}, nil
}

func (e *AgentExecutor) LatestTaskRunOutput(ctx context.Context, taskID uint64) (*TaskRunOutput, error) {
	if taskID == 0 {
		return nil, fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	task, err := e.store.LoadTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("load Task output status task_id=%d: %w", taskID, err)
	}
	result := &TaskRunOutput{
		TaskID: taskID, TaskStatus: task.Status, Running: task.Status == "executing",
	}
	paths, err := latestTaskRunOutputPaths(e.runsDir, taskID)
	if err != nil {
		return nil, err
	}
	if paths == nil {
		return result, nil
	}
	prompt, err := os.ReadFile(paths.Prompt)
	if err != nil {
		return nil, fmt.Errorf("read task prompt %q: %w", paths.Prompt, err)
	}
	stdout, err := os.ReadFile(paths.Stdout)
	if err != nil {
		return nil, fmt.Errorf("read task stdout %q: %w", paths.Stdout, err)
	}
	stderr, err := os.ReadFile(paths.Stderr)
	if err != nil {
		return nil, fmt.Errorf("read task stderr %q: %w", paths.Stderr, err)
	}
	updatedAt, err := latestModTime(paths.Prompt, paths.Stdout, paths.Stderr)
	if err != nil {
		return nil, err
	}
	result.Available = true
	result.RunKey = paths.RunKey
	result.Stage = paths.Stage
	result.Prompt = string(prompt)
	result.Stdout = string(stdout)
	result.Stderr = string(stderr)
	result.UpdatedAt = &updatedAt
	return result, nil
}

func latestTaskRunOutputPaths(runsDir string, taskID uint64) (*taskRunOutputPaths, error) {
	taskDir := filepath.Join(runsDir, fmt.Sprintf("task-%d", taskID))
	entries, err := os.ReadDir(taskDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list task output directory %q: %w", taskDir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "run-") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	sort.Strings(names)
	runKey := names[len(names)-1]
	stage, err := stageFromRunKey(runKey)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(taskDir, runKey)
	return &taskRunOutputPaths{
		RunKey: runKey,
		Stage:  stage,
		Dir:    dir,
		Prompt: filepath.Join(dir, runOutputPromptFile),
		Stdout: filepath.Join(dir, runOutputStdoutFile),
		Stderr: filepath.Join(dir, runOutputStderrFile),
	}, nil
}

func stageFromRunKey(runKey string) (string, error) {
	const prefix = "run-"
	rest := strings.TrimPrefix(runKey, prefix)
	separator := strings.IndexByte(rest, '-')
	if !strings.HasPrefix(runKey, prefix) || separator <= 0 || separator == len(rest)-1 {
		return "", fmt.Errorf("invalid task run output key %q", runKey)
	}
	if _, err := strconv.ParseInt(rest[:separator], 10, 64); err != nil {
		return "", fmt.Errorf("invalid task run output key %q: %w", runKey, err)
	}
	return rest[separator+1:], nil
}

func latestModTime(paths ...string) (time.Time, error) {
	var latest time.Time
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return time.Time{}, fmt.Errorf("stat task output %q: %w", path, err)
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest.UTC(), nil
}
