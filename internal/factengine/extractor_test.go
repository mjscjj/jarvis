package factengine

import (
	"strings"
	"testing"
	"time"
)

func TestDecodeFactsAcceptsEmptyArray(t *testing.T) {
	facts, err := DecodeFacts([]byte(`{"facts": []}`))
	if err != nil {
		t.Fatalf("DecodeFacts() error = %v", err)
	}
	if len(facts) != 0 {
		t.Fatalf("facts = %v, want empty", facts)
	}
}

// Extra keys are the prompt growing, not a protocol violation: the program reads
// the three fields it stores and leaves the rest alone.
func TestDecodeFactsIgnoresExtraKeys(t *testing.T) {
	facts, err := DecodeFacts([]byte(`{"facts": [
		{"subject_type": "group", "subject_id": 3, "description": "群里定了口径", "confidence": "high"}
	], "notes": "本轮只有一条"}`))
	if err != nil {
		t.Fatalf("DecodeFacts() error = %v", err)
	}
	if len(facts) != 1 || facts[0].SubjectID != 3 || facts[0].Description != "群里定了口径" {
		t.Fatalf("facts = %+v", facts)
	}
}

// Chat models wrap JSON in a fence often enough that losing a whole round to the
// wrapper is not worth it. The JSON inside is still held to the contract.
func TestDecodeFactsUnwrapsCodeFence(t *testing.T) {
	raw := "```json\n{\"facts\": [{\"subject_type\": \"group\", \"subject_id\": 3, \"description\": \"围栏里的事实\"}]}\n```"
	facts, err := DecodeFacts([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeFacts() error = %v", err)
	}
	if len(facts) != 1 || facts[0].Description != "围栏里的事实" {
		t.Fatalf("facts = %+v", facts)
	}
	if _, err := DecodeFacts([]byte("```json\n{not json}\n```")); err == nil {
		t.Fatal("DecodeFacts() error = nil, want malformed-body rejection")
	}
}

// The model narrates before answering often enough that the sentence in front of
// the object should not cost a round.
func TestDecodeFactsIgnoresNarrationAroundObject(t *testing.T) {
	raw := `我梳理了一下原料，有一条值得记：

{"facts": [{"subject_type": "person", "subject_id": 75, "description": "谭蕴芯承诺周三前完成分类"}]}

以上。`
	facts, err := DecodeFacts([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeFacts() error = %v", err)
	}
	if len(facts) != 1 || facts[0].SubjectID != 75 {
		t.Fatalf("facts = %+v", facts)
	}
}

// An unescaped ASCII quote inside a description desyncs the parser. It cannot be
// repaired by reading harder, so it has to fail and be retried.
func TestDecodeFactsRejectsUnescapedQuoteInDescription(t *testing.T) {
	raw := `{"facts": [{"subject_type": "group", "subject_id": 3, "description": "拆解哪些诉求是"查询+看板"能解决的"}]}`
	if _, err := DecodeFacts([]byte(raw)); err == nil {
		t.Fatal("DecodeFacts() error = nil, want parse failure")
	}
}

// A response with no facts array is malformed, not "no facts", and must not be
// read as an empty result.
func TestDecodeFactsRejectsMissingFactsKey(t *testing.T) {
	if _, err := DecodeFacts([]byte(`{}`)); err == nil ||
		!strings.Contains(err.Error(), "no facts key") {
		t.Fatalf("DecodeFacts() error = %v, want missing-facts-key failure", err)
	}
}

func TestDecodeFactsRejectsIncompleteFact(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"no subject type", `{"facts": [{"subject_id": 3, "description": "x"}]}`},
		{"zero subject id", `{"facts": [{"subject_type": "group", "subject_id": 0, "description": "x"}]}`},
		{"blank description", `{"facts": [{"subject_type": "group", "subject_id": 3, "description": "   "}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeFacts([]byte(tt.raw)); err == nil {
				t.Fatal("DecodeFacts() error = nil, want rejection")
			}
		})
	}
}

func TestDecodeFactsRejectsEmptyAndUnparseableResponses(t *testing.T) {
	for _, raw := range []string{"", "   ", "not json", `{"facts": "one"}`} {
		if _, err := DecodeFacts([]byte(raw)); err == nil {
			t.Fatalf("DecodeFacts(%q) error = nil, want rejection", raw)
		}
	}
}

func TestSourceUnitPromptCarriesSubjectsAndBody(t *testing.T) {
	unit := SourceUnit{
		Source: SourceMessage, Key: "chat-a:1-2", Context: "conversation: chat-a", Body: "10:00 张三: 我们定了用方案 B",
		Subjects: []Subject{{Type: "project", ID: 7, Name: "Jarvis"}},
	}
	prompt, err := unit.Prompt()
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	for _, want := range []string{"MATERIAL_SOURCE: message", "MATERIAL_KEY: chat-a:1-2", "CONTEXT", "conversation: chat-a", "KNOWN_ENTITIES", `"subject_type": "project"`, `"subject_id": 7`, "方案 B"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSourceUnitPromptRejectsOnlyEmptyBody(t *testing.T) {
	if _, err := (SourceUnit{Source: SourceMessage, Key: "k", Body: "  ",
		Subjects: []Subject{{Type: "group", ID: 1}}}).Prompt(); err == nil {
		t.Fatal("Prompt() error = nil, want empty-body rejection")
	}
	prompt, err := (SourceUnit{Source: SourceMessage, Key: "k", Body: "x"}).Prompt()
	if err != nil {
		t.Fatalf("Prompt() without known subjects error = %v", err)
	}
	if strings.Contains(prompt, "KNOWN_ENTITIES") || !strings.Contains(prompt, "MATERIAL:\nx") {
		t.Fatalf("prompt without known subjects = %q", prompt)
	}
}

func TestNewExtractorValidatesOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    ExtractorOptions
		wantErr string
	}{
		{"no bin", ExtractorOptions{Model: "m", Sandbox: "read-only", Timeout: time.Second}, "bin is required"},
		{"unknown bin", ExtractorOptions{Bin: "jarvis-no-such-binary", Model: "m", Sandbox: "read-only", Timeout: time.Second}, "find fact extractor binary"},
		{"no model", ExtractorOptions{Bin: "sh", Sandbox: "read-only", Timeout: time.Second}, "model is required"},
		{"bad sandbox", ExtractorOptions{Bin: "sh", Model: "m", Sandbox: "yolo", Timeout: time.Second}, "sandbox"},
		{"no timeout", ExtractorOptions{Bin: "sh", Model: "m", Sandbox: "read-only"}, "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewExtractor(tt.opts); err == nil ||
				!strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewExtractor() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
