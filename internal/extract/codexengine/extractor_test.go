package codexengine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/extract"
)

func TestCodexAnnotationTransport(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "codex")
	// Reuse this test binary as the CLI, with arguments after -- kept intact.
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=TestCodexContentHelper -- \"$@\"\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JARVIS_CODEX_CONTENT_HELPER", "1")
	extractor, err := New(Options{Bin: bin, Model: "test", Sandbox: "read-only", ReasoningEffort: "low", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	result, err := extractor.ExtractWithTools(t.Context(), extract.Prompt{System: "system", User: "user"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || !strings.Contains(string(result.Candidates[0].Annotation), `"scene":{"summary":"现场"}`) {
		t.Fatalf("lost content: %#v", result)
	}
}

func TestCodexContentHelper(t *testing.T) {
	if os.Getenv("JARVIS_CODEX_CONTENT_HELPER") != "1" {
		return
	}
	var schemaPath, outputPath string
	for i, arg := range os.Args {
		if arg == "--output-schema" {
			schemaPath = os.Args[i+1]
		}
		if arg == "--output-last-message" {
			outputPath = os.Args[i+1]
		}
	}
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatal(err)
	}
	candidate := schema["properties"].(map[string]any)["candidates"].(map[string]any)["items"].(map[string]any)
	if candidate["properties"].(map[string]any)["annotation"].(map[string]any)["type"] != "string" {
		t.Fatal("CLI schema excludes content")
	}
	output := `{"candidates":[{"action_type":"investigate","status":"extracted","title":"排查","target":"网关","project_hint":"","source_message_ids":["om_1"],"trigger_message_id":"om_1","source_quote":"请排查","payload":"明确交办","annotation":"{\"brief\":\"摘要\",\"scene\":{\"summary\":\"现场\"}}"}]}`
	if err := os.WriteFile(outputPath, []byte(output), 0600); err != nil {
		t.Fatal(err)
	}
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`)
	os.Exit(0)
}
