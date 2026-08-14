package factengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaintainerRunsOnePlainAgentSessionAtFactEngineStage(t *testing.T) {
	root := t.TempDir()
	observedPath := filepath.Join(root, "observed.txt")
	binPath := filepath.Join(root, "fake-agent")
	script := fmt.Sprintf(`#!/bin/sh
args="$*"
result=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    result="$1"
  fi
  shift
done
printf '%%s\n%%s\n%%s\n' "$PWD" "$JARVIS_AGENT_STAGE" "$args" > '%s'
printf 'updated world model' > "$result"
`, observedPath)
	if err := os.WriteFile(binPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	extractor, err := NewExtractor(ExtractorOptions{
		Bin: binPath, Model: "test", ReasoningEffort: "medium",
		Sandbox: "read-only", WorkspaceRoot: root, Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewExtractor: %v", err)
	}
	result, err := extractor.Maintain(context.Background(), "system", "material")
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if result != "updated world model" {
		t.Fatalf("result=%q", result)
	}
	observed, err := os.ReadFile(observedPath)
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(observed), resolvedRoot+"\nfactengine\n") ||
		!strings.Contains(string(observed), "-c model_reasoning_effort=medium") {
		t.Fatalf("observed workspace/stage=%q", observed)
	}
}

func TestMaintainerRejectsEmptyInputs(t *testing.T) {
	extractor := &Extractor{}
	if _, err := extractor.Maintain(t.Context(), "", "material"); err == nil {
		t.Fatal("empty system prompt accepted")
	}
	if _, err := extractor.Maintain(t.Context(), "system", ""); err == nil {
		t.Fatal("empty material prompt accepted")
	}
}

func TestSourceUnitPromptCarriesSubjectsAndBody(t *testing.T) {
	unit := SourceUnit{
		Source: SourceMessage, Key: "chat-a:1-2", Context: "conversation: chat-a", Body: "10:00 张三: 我们定了用方案 B",
		Subjects: []Subject{{Type: "project", ID: 7, Name: "Jarvis"}},
	}
	prompt, err := unit.Prompt()
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	for _, want := range []string{"MATERIAL_SOURCE: message", "MATERIAL_KEY: chat-a:1-2", "CONTEXT", "KNOWN_ENTITIES", `"subject_id": 7`, "方案 B"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSourceUnitPromptRejectsOnlyEmptyBody(t *testing.T) {
	if _, err := (SourceUnit{Source: SourceMessage, Key: "k", Body: "  "}).Prompt(); err == nil {
		t.Fatal("empty body accepted")
	}
}

func TestNewExtractorValidatesOptions(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name    string
		opts    ExtractorOptions
		wantErr string
	}{
		{"no bin", ExtractorOptions{Model: "m", Sandbox: "read-only", WorkspaceRoot: root, Timeout: time.Second}, "bin is required"},
		{"unknown bin", ExtractorOptions{Bin: "jarvis-no-such-binary", Model: "m", Sandbox: "read-only", WorkspaceRoot: root, Timeout: time.Second}, "find fact extractor binary"},
		{"no model", ExtractorOptions{Bin: "sh", Sandbox: "read-only", WorkspaceRoot: root, Timeout: time.Second}, "model is required"},
		{"bad reasoning", ExtractorOptions{Bin: "sh", Model: "m", ReasoningEffort: "ultra", Sandbox: "read-only", WorkspaceRoot: root, Timeout: time.Second}, "reasoning effort"},
		{"bad sandbox", ExtractorOptions{Bin: "sh", Model: "m", Sandbox: "yolo", WorkspaceRoot: root, Timeout: time.Second}, "sandbox"},
		{"no timeout", ExtractorOptions{Bin: "sh", Model: "m", Sandbox: "read-only", WorkspaceRoot: root}, "timeout"},
		{"no workspace", ExtractorOptions{Bin: "sh", Model: "m", Sandbox: "read-only", Timeout: time.Second}, "workspace root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewExtractor(tt.opts); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewExtractor error=%v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
