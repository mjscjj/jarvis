package textstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceListsAndReadsEveryDefinition(t *testing.T) {
	service := newTestService(t)
	items, err := service.List(t.Context())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != len(definitions()) {
		t.Fatalf("List() length = %d, want %d", len(items), len(definitions()))
	}
	for i, item := range items {
		if item.Key != definitions()[i].key {
			t.Errorf("List()[%d].Key = %q, want %q", i, item.Key, definitions()[i].key)
		}
		if item.Content != testContent(definitions()[i]) {
			t.Errorf("List()[%d].Content = %q", i, item.Content)
		}
		if item.Kind == "" || item.Stage == "" {
			t.Errorf("List()[%d] missing presentation metadata: %+v", i, item)
		}
	}
}

// The admin editor renders List verbatim, so a file registered without a name
// or description would reach the UI as a blank tab.
func TestEveryDefinitionCarriesEditorLabels(t *testing.T) {
	for _, item := range definitions() {
		if strings.TrimSpace(item.name) == "" || strings.TrimSpace(item.description) == "" {
			t.Errorf("definition %q needs both a name and a description, got %+v", item.key, item)
		}
	}
}

func TestWeeklyReportDefinitionsAreEditableMarkdown(t *testing.T) {
	service := newTestService(t)
	want := []string{
		WeeklyReportReminderTemplateKey,
	}
	for _, key := range want {
		item, err := service.Get(t.Context(), key)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", key, err)
		}
		if item.Stage != "weekly_report" {
			t.Errorf("Get(%q).Stage = %q, want weekly_report", key, item.Stage)
		}
		if item.Kind != "message_template" {
			t.Errorf("Get(%q).Kind = %q", key, item.Kind)
		}
	}
}

func TestRepositoryWeeklyReportReminderTemplateListsReviewIssues(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "conf", "prompts", "weekly-report-reminder-template.md"))
	if err != nil {
		t.Fatalf("read reminder template: %v", err)
	}
	template := string(content)
	for _, placeholder := range []string{
		"{{owner_name}}",
		"{{missing_progress_section}}",
		"{{missing_core_section}}",
		"{{missing_score_section}}",
		"{{unchanged_section}}",
		"{{week}}",
		"{{count}}",
		"{{kr_title}}",
		"{{fill_url}}",
	} {
		if !strings.Contains(template, placeholder) {
			t.Errorf("reminder template missing review issue placeholder %s", placeholder)
		}
	}
	for _, unavailable := range []string{"{{owner_mention}}", "{{missing_items}}", "{{due_at}}", "\n同步："} {
		if strings.Contains(template, unavailable) {
			t.Errorf("reminder template contains unsupported content %s", unavailable)
		}
	}
}

func TestOKRWeeklyReminderUsesBroadcastDelivery(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "conf", "prompts", "okr-agent-weekly-reminder.md"))
	if err != nil {
		t.Fatalf("read OKR weekly reminder prompt: %v", err)
	}
	prompt := string(content)
	for _, want := range []string{
		"`feishu-broadcast` Skill", "Jarvis通知机器人", "企业邮箱", "不得搜索、复用或创建助手群",
		"`Platform Team Weekly Catch Up`", "`yield-until`", "前一个自然日的 19:30", "前 4 小时",
		"`[Core Group] Platform Team`", "`feishu-send-message` Skill", "按 O-KR 维度", "请尽快更新：https://emily.bytedance.net/#/weekly-report?quarter=2026-Q3&tab=review-fill",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("OKR weekly reminder prompt missing broadcast contract %q:\n%s", want, prompt)
		}
	}
	for _, obsolete := range []string{"okr_review_reminder_channel_for", "群复用/新建数量"} {
		if strings.Contains(prompt, obsolete) {
			t.Fatalf("OKR weekly reminder prompt still contains assistant-group delivery %q:\n%s", obsolete, prompt)
		}
	}
}

func TestWeeklyReportReminderSkillUsesTwoBoardsAndFinalPrompt(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "weekly-report-reminder", "SKILL.md"))
	if err != nil {
		t.Fatalf("read weekly report reminder skill: %v", err)
	}
	skill := string(content)
	for _, want := range []string{
		"okr_agent_weekly_reminder",
		"lark-cli calendar +search-event",
		"jarvis-tools yield-until",
		"board --quarter '<quarter>' --week '<week>'",
		"board --quarter '<quarter>' --week '<previous_week>'",
		"`previous_week`",
		"missing_progress_section",
		"missing_core_section",
		"missing_score_section",
		"unchanged_section",
		"`feishu-send-message` Skill",
		"<at user_id=\"...\">姓名</at>",
	} {
		if !strings.Contains(skill, want) {
			t.Fatalf("weekly report reminder skill missing current contract %q:\n%s", want, skill)
		}
	}
	for _, obsolete := range []string{"reminder-preview", "create-reminder-batch", "reminder-batches", "missing_items"} {
		if strings.Contains(skill, obsolete) {
			t.Fatalf("weekly report reminder skill still contains obsolete contract %q:\n%s", obsolete, skill)
		}
	}
}

func TestOKRAgentDefinitionsAreEditableMarkdown(t *testing.T) {
	service := newTestService(t)
	want := []string{
		OKRAgentPrinciplesKey,
		OKRAgentQuarterlyDraftKey,
		OKRAgentRegionAlignmentKey,
		OKRAgentMeegoAlignmentKey,
		OKRAgentReportAKey,
		OKRAgentReportBKey,
		OKRAgentReportCKey,
		OKRAgentWeeklyReminderKey,
		OKRAgentProgressSyncKey,
		OKRAgentPreviewReviewKey,
	}
	for _, key := range want {
		item, err := service.Get(t.Context(), key)
		if err != nil {
			t.Fatalf("Get(%q) error = %v", key, err)
		}
		if item.Stage != "okr_agent" {
			t.Errorf("Get(%q).Stage = %q, want okr_agent", key, item.Stage)
		}
		if item.Kind != "agent_policy" && item.Kind != "agent_prompt" {
			t.Errorf("Get(%q).Kind = %q", key, item.Kind)
		}
	}
}

func TestServiceUpdateAtomicallyReplacesContent(t *testing.T) {
	service := newTestService(t)
	updated, err := service.Update(t.Context(), SystemPromptM5Key, Input{Content: " 第一行\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}\n第二行 "})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Content != "第一行\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}\n第二行" {
		t.Fatalf("Update().Content = %q", updated.Content)
	}
	onDisk, err := os.ReadFile(updated.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(onDisk) != "第一行\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}\n第二行\n" {
		t.Fatalf("on-disk content = %q", onDisk)
	}
}

func TestServiceRejectsUnknownKeyAndEmptyContent(t *testing.T) {
	service := newTestService(t)
	if _, err := service.Get(t.Context(), "../secret"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrNotFound", err)
	}
	if _, err := service.Update(t.Context(), SystemPromptM5Key, Input{Content: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update(empty) error = %v, want ErrInvalidInput", err)
	}
}

func TestServiceRejectsInvalidSystemPromptTemplateWithoutReplacingFile(t *testing.T) {
	service := newTestService(t)
	before, err := service.Content(t.Context(), SystemPromptM5Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(t.Context(), SystemPromptM5Key, Input{Content: "missing placeholders"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update(invalid template) error = %v, want ErrInvalidInput", err)
	}
	after, err := service.Content(t.Context(), SystemPromptM5Key)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("invalid update replaced file: before=%q after=%q", before, after)
	}
}

func TestNewServiceFailsWhenRequiredFileIsMissingOrEmpty(t *testing.T) {
	directory := t.TempDir()
	writeDefinitions(t, directory)
	if err := os.Remove(filepath.Join(directory, definitions()[0].filename)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(directory); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NewService(missing) error = %v, want ErrNotFound", err)
	}

	writeDefinitions(t, directory)
	if err := os.WriteFile(filepath.Join(directory, definitions()[1].filename), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(directory); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewService(empty) error = %v, want ErrInvalidInput", err)
	}
}

func TestRepositoryPromptsDoNotEmbedToolManuals(t *testing.T) {
	service, err := NewService(filepath.Join("..", "..", "conf", "prompts"))
	if err != nil {
		t.Fatalf("NewService(repository prompts) error = %v", err)
	}
	for _, key := range []string{SystemPromptM3Key, SystemPromptM5Key, SystemPromptChatKey, SystemPromptProactiveKey} {
		content, err := service.Content(t.Context(), key)
		if err != nil {
			t.Fatalf("Content(%q) error = %v", key, err)
		}
		for _, toolName := range []string{"jarvis-tools", "lark-cli", "bytedcli"} {
			if strings.Contains(content, toolName) {
				t.Fatalf("%s must not embed tool manual %q", key, toolName)
			}
		}
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	directory := t.TempDir()
	writeDefinitions(t, directory)
	service, err := NewService(directory)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func writeDefinitions(t *testing.T, directory string) {
	t.Helper()
	for _, item := range definitions() {
		path := filepath.Join(directory, item.filename)
		if err := os.WriteFile(path, []byte(testContent(item)+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func testContent(item definition) string {
	switch item.key {
	case SystemPromptM3Key:
		return "initial " + item.key + "\n{{WORK_RULES}}"
	case SystemPromptM5Key:
		return "initial " + item.key + "\n{{WORK_RULES}}\n{{APPROVAL_POLICY}}"
	default:
		return "initial " + item.key
	}
}
