package dailydigest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeSummaryRunner struct {
	output  string
	err     error
	prompt  string
	sandbox string
	calls   int
}

func (f *fakeSummaryRunner) RunTextSandbox(_ context.Context, prompt, sandbox string) (string, error) {
	f.calls++
	f.prompt = prompt
	f.sandbox = sandbox
	return f.output, f.err
}

func (f *fakeSummaryRunner) RunTextSandboxAt(
	ctx context.Context,
	prompt, sandbox, _ string,
) (string, error) {
	return f.RunTextSandbox(ctx, prompt, sandbox)
}

func TestGroupGenerateUsesCodexSkillAndReportsCoverage(t *testing.T) {
	t.Parallel()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE message (
			id INTEGER PRIMARY KEY,
			message_id TEXT NOT NULL,
			group_id INTEGER,
			sender_name TEXT,
			sender_type TEXT,
			message_type TEXT,
			content TEXT,
			reply_to TEXT,
			root_id TEXT,
			thread_id TEXT,
			create_time INTEGER
		)
	`).Error; err != nil {
		t.Fatalf("create message table: %v", err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	start, err := time.ParseInLocation("2006-01-02", "2026-07-23", location)
	if err != nil {
		t.Fatalf("parse day: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO message(id, message_id, group_id, sender_name, sender_type, message_type, content, create_time)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "om_1", 9, "Alice", "user", "text", "MR 已合入：https://example.com/mr/1", start.Add(10*time.Hour).UnixMilli(),
	).Error; err != nil {
		t.Fatalf("insert message: %v", err)
	}

	runner := &fakeSummaryRunner{output: `{
		"summary":"# 核心群 2026-07-23\n\n## 一句话结论\nMR 已合入。\n\n## 材料\n- https://example.com/mr/1",
		"sources":{
			"lark_group_messages":{"status":"ok","count":3},
			"lark_documents":{"status":"empty","count":0},
			"code_commits":{"status":"ok","count":1},
			"code_mrs":{"status":"ok","count":1},
			"other_materials":{"status":"empty","count":0}
		}
	}`}
	generator := &groupGenerator{
		db:           db,
		runner:       runner,
		location:     location,
		messageLimit: 200,
		skillText:    "GROUP SUMMARY SKILL",
		sandbox:      "danger-full-access",
	}
	cutoff := start.Add(18 * time.Hour)
	result, err := generator.Generate(context.Background(), 9, "核心群", "oc_x", "2026-07-23", start, start.AddDate(0, 0, 1), cutoff)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if runner.calls != 1 || runner.sandbox != "danger-full-access" {
		t.Fatalf("runner calls=%d sandbox=%q", runner.calls, runner.sandbox)
	}
	for _, want := range []string{"GROUP SUMMARY SKILL", "oc_x", "om_1", "lark-cli"} {
		if !strings.Contains(runner.prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, runner.prompt)
		}
	}
	if result.Coverage["jarvis_group_messages"].Count != 1 {
		t.Fatalf("jarvis coverage = %#v", result.Coverage["jarvis_group_messages"])
	}
	if result.SourceCount != 5 {
		t.Fatalf("source count = %d, want 5", result.SourceCount)
	}
	if !result.CutoffAt.Equal(cutoff) {
		t.Fatalf("cutoff = %s, want %s", result.CutoffAt, cutoff)
	}
}

func TestGroupGenerateStillRunsCodexWhenJarvisHasNoMessages(t *testing.T) {
	t.Parallel()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE message (
			id INTEGER PRIMARY KEY,
			group_id INTEGER,
			create_time INTEGER
		)
	`).Error; err != nil {
		t.Fatalf("create message table: %v", err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	start, err := time.ParseInLocation("2006-01-02", "2026-07-23", location)
	if err != nil {
		t.Fatalf("parse day: %v", err)
	}
	runner := &fakeSummaryRunner{output: `{
		"summary":"该群当天无可确认的实质进展。",
		"sources":{
			"lark_group_messages":{"status":"empty","count":0},
			"lark_documents":{"status":"empty","count":0},
			"code_commits":{"status":"empty","count":0},
			"code_mrs":{"status":"empty","count":0},
			"other_materials":{"status":"empty","count":0}
		}
	}`}
	generator := &groupGenerator{
		db:           db,
		runner:       runner,
		location:     location,
		messageLimit: 200,
		skillText:    "GROUP SUMMARY SKILL",
		sandbox:      "danger-full-access",
	}
	if _, err := generator.Generate(context.Background(), 9, "核心群", "oc_x", "2026-07-23", start, start.AddDate(0, 0, 1), start.Add(18*time.Hour)); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if !strings.Contains(runner.prompt, "不代表群里没有消息") {
		t.Fatalf("zero-message prompt missing lark fallback:\n%s", runner.prompt)
	}
}

func TestValidateGroupRunnerOutputNormalizesSuccessStatus(t *testing.T) {
	t.Parallel()
	output := &groupRunnerOutput{
		Summary: "当天已完成群消息查询。",
		Sources: SourceCoverage{
			"lark_group_messages": {Status: "success", Count: 2},
			"lark_documents":      {Status: "empty", Count: 0},
			"code_commits":        {Status: "empty", Count: 0},
			"code_mrs":            {Status: "empty", Count: 0},
			"other_materials":     {Status: "empty", Count: 0},
		},
	}

	if err := validateGroupRunnerOutput(output); err != nil {
		t.Fatalf("validate group output: %v", err)
	}
	if got := output.Sources["lark_group_messages"].Status; got != "ok" {
		t.Fatalf("normalized lark group message status = %q, want ok", got)
	}
}
