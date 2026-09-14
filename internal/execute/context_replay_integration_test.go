//go:build integration

package execute

import (
	"context"
	"encoding/json"
	"jarvis/internal/contextpack"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in: uses a real model with fixed evidence and no permitted tool
// actions. The retained prompt/result are inspected separately from unit tests.
func TestContextReplayLive(t *testing.T) {
	if os.Getenv("JARVIS_CONTEXT_REPLAY") != "1" {
		t.Skip("set JARVIS_CONTEXT_REPLAY=1 for real model replay")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	system, err := os.ReadFile(filepath.Join(root, "conf/prompts/m5-system-prompt.md"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := os.ReadFile(filepath.Join(root, "conf/prompts/m5-approval-policy.md"))
	if err != nil {
		t.Fatal(err)
	}
	artifacts := filepath.Join(root, "runs", "context-replay")
	if err := os.MkdirAll(artifacts, 0700); err != nil {
		t.Fatal(err)
	}
	capture := []byte(`{"captured_at":"2026-09-13T08:00:00Z","group":{"chat_mode":"p2p","peer_name":"张若怡","p2p_target_type":"user"},"messages":[{"message_id":"ask","sender_name":"储节节","content":"你现在能看到么"},{"message_id":"reply","sender_name":"张若怡","content":"可以的","reply_to":"ask"}]}`)
	cases := []struct{ name, origin, source, annotation, want string }{
		{"private", "todo", `{"source_message_ids":["ask"],"payload":"Principal授权Jarvis检查网页并发消息"}`, `{"brief":"去检查网页并汇报"}`, "observing"},
		{"manual", "manual", `{"instruction":"根据所附对话指出询问对象是谁，只写本任务结果，不发送消息。"}`, `{}`, "completed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			packet, err := contextpack.Freeze([]byte(tc.source), capture, "OLD_ADMISSION", []byte(tc.annotation))
			if err != nil {
				t.Fatal(err)
			}
			task := &domain.Task{ID: 900001, Title: "页面可见性对话", ActionType: "investigate", SourceType: tc.origin, SourcePayload: datatypes.JSON(packet), Status: "pending"}
			prompt, err := buildExecutionPrompt(executionPromptInput{SystemPrompt: string(system), ApprovalPolicy: string(policy), InitiativeLevel: "normal", Task: task, WorldOverview: json.RawMessage(`{"principal":{"name":"储节节"},"sections":[],"note":"fixed replay fixture"}`)})
			if err != nil {
				t.Fatal(err)
			}
			prompt += "\n本轮是固定证据回放，所有材料已给出。禁止调用工具、查询外部系统或产生任何副作用。根据现场及原始请求返回最终结果。"
			dir, err := os.MkdirTemp(artifacts, tc.name+"-")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte(prompt), 0600); err != nil {
				t.Fatal(err)
			}
			schemaPath := filepath.Join(dir, "schema.json")
			if err := os.WriteFile(schemaPath, []byte(executionResultSchema), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "codex", "exec", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only", "-c", "model_reasoning_effort=\"low\"", "--output-schema", schemaPath, "--output-last-message", filepath.Join(dir, "result.json"), "--json", "-")
			cmd.Dir = t.TempDir()
			cmd.Stdin = strings.NewReader(prompt)
			output, err := cmd.CombinedOutput()
			if writeErr := os.WriteFile(filepath.Join(dir, "stdout.jsonl"), output, 0600); writeErr != nil {
				t.Fatal(writeErr)
			}
			if err != nil {
				t.Fatalf("replay failed: %v; artifacts %s", err, dir)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "result.json"))
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Outcome string            `json:"outcome"`
				Summary string            `json:"summary"`
				Effects []json.RawMessage `json:"effects"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			if result.Outcome != tc.want || len(result.Effects) != 0 || !strings.Contains(result.Summary, "张若怡") {
				t.Fatalf("unexpected behavior: %s; artifacts %s", raw, dir)
			}
			t.Logf("%s: %s; artifacts %s", tc.name, result.Summary, dir)
		})
	}
}
