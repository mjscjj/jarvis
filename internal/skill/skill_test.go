package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type testAvailability map[string]bool

func (a testAvailability) SkillEnabled(_ context.Context, name string) (bool, error) {
	enabled, owned := a[name]
	if !owned {
		return true, nil
	}
	return enabled, nil
}

func TestParseMetadata(t *testing.T) {
	meta, err := parseMetadata([]byte("---\nname: example\ndescription: example skill\n---\n\n# Body\n"))
	if err != nil {
		t.Fatalf("parseMetadata() error = %v", err)
	}
	if meta.Name != "example" || meta.Description != "example skill" {
		t.Fatalf("metadata = %#v", meta)
	}
}

func TestNormalizeStages(t *testing.T) {
	stages, err := normalizeStages([]string{StageExecute, StageExtract, StageExecute})
	if err != nil {
		t.Fatalf("normalizeStages() error = %v", err)
	}
	if !reflect.DeepEqual(stages, []string{StageExtract, StageExecute}) {
		t.Fatalf("stages = %#v", stages)
	}
	if _, err := normalizeStages(nil); err == nil {
		t.Fatal("normalizeStages(nil) must fail")
	}
	if _, err := normalizeStages([]string{"unknown"}); err == nil {
		t.Fatal("normalizeStages(unknown) must fail")
	}
}

func TestServiceReadsAndUpdatesConfiguration(t *testing.T) {
	root, configPath := writeSkillFixture(t, false)
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	items, err := service.List(t.Context())
	if err != nil || len(items) != 1 || items[0].Name != "example" {
		t.Fatalf("List() = %#v err=%v", items, err)
	}
	enabled := false
	updated, err := service.Update(t.Context(), "example", Input{
		Stages: []string{StageExtract, StageExecute}, IsEnabled: &enabled,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.IsEnabled || !reflect.DeepEqual(updated.Stages, []string{StageExtract, StageExecute}) {
		t.Fatalf("Update() = %#v", updated)
	}
	reloaded, err := NewService(root, configPath)
	if err != nil {
		t.Fatalf("reload service: %v", err)
	}
	items, err = reloaded.List(t.Context())
	if err != nil || items[0].IsEnabled {
		t.Fatalf("reloaded List() = %#v err=%v", items, err)
	}
}

func TestServiceUpdatesSkillMarkdownWithRevisionGuard(t *testing.T) {
	root := t.TempDir()
	skillDirectory := filepath.Join(root, "product-prd-review")
	if err := os.Mkdir(skillDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "---\nname: product-prd-review\ndescription: Review product docs\nmodule: product-management\n---\n\n# Original\n"
	path := filepath.Join(skillDirectory, "SKILL.md")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: product-prd-review\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.EditableContent(t.Context(), "product-prd-review")
	if err != nil {
		t.Fatal(err)
	}
	updatedText := strings.Replace(original, "# Original", "# Updated", 1)
	updated, err := service.UpdateContent(t.Context(), "product-prd-review", ContentInput{
		Content: updatedText, ExpectedRevision: current.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != updatedText || updated.Revision == current.Revision {
		t.Fatalf("updated = %#v", updated)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != updatedText {
		t.Fatalf("file = %q err=%v", raw, err)
	}
	if _, err := service.UpdateContent(t.Context(), "product-prd-review", ContentInput{
		Content: original, ExpectedRevision: current.Revision,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale UpdateContent() error = %v, want ErrConflict", err)
	}
}

func TestServiceRejectsSkillIdentityChangesAndEmptyBody(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "product-prd-review")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "---\nname: product-prd-review\ndescription: Review product docs\nmodule: product-management\n---\n\n# Original\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: product-prd-review\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := service.EditableContent(t.Context(), "product-prd-review")
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{
		strings.Replace(original, "name: product-prd-review", "name: product-tools", 1),
		strings.Replace(original, "module: product-management", "module: okr", 1),
		"---\nname: product-prd-review\ndescription: Review product docs\nmodule: product-management\n---\n",
	} {
		if _, err := service.UpdateContent(t.Context(), "product-prd-review", ContentInput{
			Content: content, ExpectedRevision: current.Revision,
		}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("UpdateContent(%q) error = %v, want ErrInvalidInput", content, err)
		}
	}
}

func TestDisabledCatalogEntryRemainsReadable(t *testing.T) {
	for _, inline := range []bool{false, true} {
		root, configPath := writeSkillFixture(t, inline)
		service, err := NewService(root, configPath)
		if err != nil {
			t.Fatal(err)
		}
		enabled := false
		if _, err := service.Update(t.Context(), "example", Input{Stages: []string{StageExtract}, IsEnabled: &enabled}); err != nil {
			t.Fatal(err)
		}
		catalog, err := service.Catalog(t.Context(), StageExtract)
		if err != nil || catalog != "" {
			t.Fatalf("disabled catalog=%q err=%v", catalog, err)
		}
		content, err := service.Content(t.Context(), "example")
		if err != nil || !strings.Contains(content.Content, "BODY_MARKER") {
			t.Fatalf("content=%v err=%v", content, err)
		}
	}
}

func TestAvailabilityGateHidesSkill(t *testing.T) {
	root, configPath := writeSkillFixture(t, false)
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	service.SetAvailability(testAvailability{"example": false})
	items, err := service.List(t.Context())
	if err != nil || len(items) != 1 || !items[0].IsEnabled || items[0].IsAvailable {
		t.Fatalf("List() = %#v err=%v", items, err)
	}
	if _, err := service.Content(t.Context(), "example"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Content() error = %v, want ErrNotFound", err)
	}
}

func TestRenderingServiceDoesNotChangeSource(t *testing.T) {
	root, configPath := writeSkillFixture(t, false)
	source, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewRenderingService(source, func(value string) string {
		return strings.ReplaceAll(value, "{{AGENT_NAME}}", "小贾")
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := service.Catalog(t.Context(), StageExecute)
	if err != nil {
		t.Fatal(err)
	}
	content, err := service.Content(t.Context(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(catalog, "小贾 skill") || !strings.Contains(content.Content, "# 小贾 body") {
		t.Fatalf("catalog=%q content=%q", catalog, content.Content)
	}
	raw, err := source.Content(t.Context(), "example")
	if err != nil || !strings.Contains(raw.Content, "{{AGENT_NAME}}") {
		t.Fatalf("source content changed: %#v err=%v", raw, err)
	}
	editable, err := service.EditableContent(t.Context(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(editable.Content, "{{AGENT_NAME}}") {
		t.Fatalf("editable content was rendered: %q", editable.Content)
	}
}

func TestServiceFailsWhenSkillConfigurationDoesNotMatchFiles(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: missing-skill\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(root, configPath); err == nil {
		t.Fatal("stale configured skill must fail")
	}
}

func TestModuleOwnedSkillIsNotInjectedWhileModuleDisabled(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "okr-world-projector")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: okr-world-projector\ndescription: project OKR\nmodule: okr\n---\n\n# Body\n"
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: okr-world-projector\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root, configPath, WithModuleGate(func(_ context.Context, key string) (bool, error) {
		return key != "okr", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	items, err := service.List(t.Context())
	if err != nil || len(items) != 1 || items[0].IsAvailable {
		t.Fatalf("List() = %#v, %v", items, err)
	}
	catalog, err := service.Catalog(t.Context(), StageExecute)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(catalog, "okr-world-projector") {
		t.Fatalf("disabled module skill leaked into catalog: %s", catalog)
	}
	if _, err := service.Content(t.Context(), "okr-world-projector"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled module skill content error = %v, want ErrNotFound", err)
	}
}

func TestRepositoryFeishuApprovalCardIsOwnedByServer(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "feishu-send-message", "SKILL.md"))
	if err != nil {
		t.Fatalf("read repository Feishu message skill: %v", err)
	}
	skill := string(content)
	for _, want := range []string{
		"审批通知不由这个 Skill 发送",
		"不要用本 Skill 发送请示卡片或纯文字提醒",
		"先持久化 `question` 和 `needs_human` 状态",
		"绑定当前 Task version",
		"当前是 `resume_human`",
	} {
		if !strings.Contains(skill, want) {
			t.Fatalf("Feishu message skill missing approval-card contract %q:\n%s", want, skill)
		}
	}
	for _, obsolete := range []string{
		`"action": "jarvis_approval"`,
		`--msg-type interactive`,
		"卡片连续发送失败就退回",
		"awaiting_approval",
		"APPROVED_PROPOSAL",
		"当前是 apply 阶段",
	} {
		if strings.Contains(skill, obsolete) {
			t.Fatalf("Feishu message skill still contains obsolete approval-card rule %q:\n%s", obsolete, skill)
		}
	}
}

func TestRepositoryFeishuMessageSkillDefinesM5SendClosure(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "feishu-send-message", "SKILL.md"))
	if err != nil {
		t.Fatalf("read repository Feishu message skill: %v", err)
	}
	skill := string(content)
	for _, want := range []string{
		"本 Skill 负责原会话中的业务回复",
		"面向多个独立收件人的系统通知或批量提醒读取 `feishu-broadcast`",
		"给 principal 本人的主动通知和动作回执使用 `jarvis-tools notice-principal`",
		"不执行本 Skill 的任何写命令",
		"jarvis-tools get-agent-identity",
		"同一运行实例的有效身份",
		"lark-cli auth status --json --verify",
		"lark-cli 当前默认身份",
		"user `openId` 与 principal `open_id` 完全相同",
		"+chat-search",
		"--page-token",
		"+chat-members-list",
		"--page-all --page-limit 0",
		"多个候选、成员不完整或用途不确定时停止",
		"发送失败原样报错，不换身份或会话",
		"JARVIS_TASK_ID",
		"飞书幂等窗口只有一小时",
		"+messages-mget",
		"message_id",
		"anchor_message_id",
		"idempotency_key",
		"effects 是 M5 根据已核验工具结果作出的展示申报",
		`at user_id="<principal open_id>"`,
		"同时真实 `@` 对方和 principal",
	} {
		if !strings.Contains(skill, want) {
			t.Fatalf("Feishu message skill missing send contract %q:\n%s", want, skill)
		}
	}
	for _, forbidden := range []string{
		"如果 `--user-id` 直发失败（极少见，说明还没建立私聊关系），再按下面",
		"user_message",
		"lark_profile",
		"--profile",
	} {
		if strings.Contains(skill, forbidden) {
			t.Fatalf("Feishu message skill still contains obsolete send rule %q:\n%s", forbidden, skill)
		}
	}
}

func TestRepositoryFeishuMessageSkillIsNotExposedToExtract(t *testing.T) {
	service, err := NewService(
		filepath.Join("..", "..", ".agents", "skills"),
		filepath.Join("..", "..", "conf", "skills.yaml"),
	)
	if err != nil {
		t.Fatalf("load repository skills: %v", err)
	}
	extractCatalog, err := service.Catalog(t.Context(), StageExtract)
	if err != nil {
		t.Fatalf("extract Catalog() error = %v", err)
	}
	if strings.Contains(extractCatalog, "feishu-send-message") {
		t.Fatalf("extract catalog exposes feishu-send-message:\n%s", extractCatalog)
	}
	executeCatalog, err := service.Catalog(t.Context(), StageExecute)
	if err != nil {
		t.Fatalf("execute Catalog() error = %v", err)
	}
	if !strings.Contains(executeCatalog, "feishu-send-message") {
		t.Fatalf("execute catalog is missing feishu-send-message:\n%s", executeCatalog)
	}
}

func TestRepositoryFeishuBroadcastSkillIsExecuteOnlyAndOwnsDirectDelivery(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "feishu-broadcast", "SKILL.md"))
	if err != nil {
		t.Fatalf("read repository Feishu broadcast skill: %v", err)
	}
	skill := string(content)
	for _, want := range []string{
		"Jarvis通知机器人",
		"cli_a96a2422f03bdbd7",
		"不搜索、复用或创建助手群",
		"/open-apis/message/v4/batch_send/",
		"/get_progress",
		"个性化广播",
		"im:message:send_multi_users",
		"union_id",
		"enterprise_email",
		"receive_id_type",
		"open_id` 按飞书 App 隔离",
	} {
		if !strings.Contains(skill, want) {
			t.Fatalf("Feishu broadcast skill missing delivery contract %q:\n%s", want, skill)
		}
	}
	service, err := NewService(
		filepath.Join("..", "..", ".agents", "skills"),
		filepath.Join("..", "..", "conf", "skills.yaml"),
	)
	if err != nil {
		t.Fatalf("load repository skills: %v", err)
	}
	extractCatalog, err := service.Catalog(t.Context(), StageExtract)
	if err != nil {
		t.Fatalf("extract Catalog() error = %v", err)
	}
	if strings.Contains(extractCatalog, "feishu-broadcast") {
		t.Fatalf("extract catalog exposes feishu-broadcast:\n%s", extractCatalog)
	}
	executeCatalog, err := service.Catalog(t.Context(), StageExecute)
	if err != nil {
		t.Fatalf("execute Catalog() error = %v", err)
	}
	if !strings.Contains(executeCatalog, "feishu-broadcast") {
		t.Fatalf("execute catalog is missing feishu-broadcast:\n%s", executeCatalog)
	}
}

func TestInlineSkillRendersBodyAndRespectsAvailability(t *testing.T) {
	root, configPath := writeSkillFixture(t, true)
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := service.Catalog(t.Context(), StageExtract)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BEGIN_INLINE_SKILL name=example", "# {{AGENT_NAME}} body", "BODY_MARKER"} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("inline catalog missing %q:\n%s", want, catalog)
		}
	}
	service.SetAvailability(testAvailability{"example": false})
	catalog, err = service.Catalog(t.Context(), StageExtract)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(catalog, "BODY_MARKER") || strings.Contains(catalog, "name=example") {
		t.Fatalf("disabled inline skill leaked into catalog:\n%s", catalog)
	}
}

func TestRepositoryChatSkillIsReadableWithoutStageInjection(t *testing.T) {
	svc, err := NewService(filepath.Join("..", "..", ".agents", "skills"), filepath.Join("..", "..", "conf", "skills.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := svc.Content(t.Context(), "jarvis-chat")
	if err != nil || content == nil || strings.TrimSpace(content.Content) == "" {
		t.Fatalf("chat Skill must be readable on demand: %v", err)
	}
	for _, stage := range []string{StageExtract, StageExecute, StageProactive} {
		catalog, err := svc.Catalog(t.Context(), stage)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(catalog, "jarvis-chat") {
			t.Fatalf("chat Skill leaked into %s", stage)
		}
	}
}

func TestRepositoryInstallationSkillsAreStandalone(t *testing.T) {
	service, err := NewService(
		filepath.Join("..", "..", ".agents", "skills"),
		filepath.Join("..", "..", "conf", "skills.yaml"),
	)
	if err != nil {
		t.Fatalf("load repository skills: %v", err)
	}
	items, err := service.List(t.Context())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	found := map[string]bool{
		"bootstrap-jarvis-world-model": false,
		"install-jarvis":               false,
	}
	for _, item := range items {
		if _, exists := found[item.Name]; !exists {
			continue
		}
		found[item.Name] = true
		if item.IsEnabled {
			t.Fatalf("%s must stay disabled in the Jarvis runtime catalog", item.Name)
		}
	}
	for name, wasFound := range found {
		if !wasFound {
			t.Fatalf("%s repository skill was not discovered", name)
		}
	}
	executeCatalog, err := service.Catalog(t.Context(), StageExecute)
	if err != nil {
		t.Fatalf("execute Catalog() error = %v", err)
	}
	for name := range found {
		if strings.Contains(executeCatalog, name) {
			t.Fatalf("M5 catalog exposes standalone %s:\n%s", name, executeCatalog)
		}
	}
}

func TestRepositoryWeeklyReportProgressSyncSkillKeepsReadOnlyTaskBoundary(t *testing.T) {
	service, err := NewService(
		filepath.Join("..", "..", ".agents", "skills"),
		filepath.Join("..", "..", "conf", "skills.yaml"),
	)
	if err != nil {
		t.Fatalf("load repository skills: %v", err)
	}
	extractCatalog, err := service.Catalog(t.Context(), StageExtract)
	if err != nil {
		t.Fatalf("extract Catalog() error = %v", err)
	}
	if strings.Contains(extractCatalog, "weekly-report-progress-sync") {
		t.Fatalf("extract catalog exposes execution-only OKR sync skill:\n%s", extractCatalog)
	}
	executeCatalog, err := service.Catalog(t.Context(), StageExecute)
	if err != nil {
		t.Fatalf("execute Catalog() error = %v", err)
	}
	if !strings.Contains(executeCatalog, "weekly-report-progress-sync") {
		t.Fatalf("execute catalog is missing weekly-report-progress-sync:\n%s", executeCatalog)
	}

	content, err := service.Content(t.Context(), "weekly-report-progress-sync")
	if err != nil {
		t.Fatalf("load weekly-report-progress-sync: %v", err)
	}
	for _, want := range []string{
		"外部系统只读",
		"scripts/biz-okr-tools board",
		"scripts/biz-okr-tools record-meego-observation",
		"jarvis-tools append-clue",
		"jarvis-tools append-fact",
		"jarvis-tools list-relations",
		"create-relation",
		"标题相似、同负责人或同一群都不能单独作为自动关联依据",
		"Page CAS",
		"ScheduledTask",
		"每次触发只创建一个普通 Task",
		"不发送消息",
		"不调用 `create-task`",
	} {
		if !strings.Contains(content.Content, want) {
			t.Fatalf("OKR progress sync skill missing boundary %q:\n%s", want, content.Content)
		}
	}
}

func TestRepositoryBaxWeeklyUsageAnalysisSkillIsExecutable(t *testing.T) {
	service, err := NewService(
		filepath.Join("..", "..", ".agents", "skills"),
		filepath.Join("..", "..", "conf", "skills.yaml"),
	)
	if err != nil {
		t.Fatalf("load repository skills: %v", err)
	}
	content, err := service.Content(t.Context(), "bax-weekly-usage-analysis")
	if err != nil {
		t.Fatalf("Content(bax-weekly-usage-analysis) error = %v", err)
	}
	skill := content.Content
	for _, want := range []string{
		"按周分析 BAX AM 用户对话使用数据",
		"bytedcli --site i18n-tt --json aeolus dataset-fields -r sg 3574811",
		"dry-run.sh",
		"周期任务只需把 instruction 写成",
		"不要为 BAX 周报新建 Go 专用链路",
		"未获批准",
	} {
		if !strings.Contains(skill, want) {
			t.Fatalf("BAX weekly usage skill missing contract %q:\n%s", want, skill)
		}
	}
	catalog, err := service.Catalog(t.Context(), StageExecute)
	if err != nil {
		t.Fatalf("execute Catalog() error = %v", err)
	}
	if !strings.Contains(catalog, "bax-weekly-usage-analysis") {
		t.Fatalf("execute catalog is missing bax-weekly-usage-analysis:\n%s", catalog)
	}
	extractCatalog, err := service.Catalog(t.Context(), StageExtract)
	if err != nil {
		t.Fatalf("extract Catalog() error = %v", err)
	}
	if strings.Contains(extractCatalog, "bax-weekly-usage-analysis") {
		t.Fatalf("extract catalog exposes bax-weekly-usage-analysis:\n%s", extractCatalog)
	}
}

func TestBootstrapJarvisWorldModelUsesUserAuthoredDocumentsInsteadOfOKRAPI(t *testing.T) {
	worldModelPath := filepath.Join("..", "..", ".agents", "skills", "bootstrap-jarvis-world-model")
	worldModelSkill, err := os.ReadFile(filepath.Join(worldModelPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	evidenceSources, err := os.ReadFile(filepath.Join(worldModelPath, "references", "evidence-sources.md"))
	if err != nil {
		t.Fatal(err)
	}
	installSkill, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "install-jarvis", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	combined := string(worldModelSkill) + "\n" + string(evidenceSources)
	for _, want := range []string{
		"`lark-drive`、`lark-doc`",
		"--created-by-me",
		"--query \"\" --edited-since <from>",
		"`docs +fetch`",
		"不得改走 OKR API",
		"search:docs:read",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("document-based initialization contract is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"加载并遵循 `lark-contact`、`lark-okr`",
		"当前 OKR 周期、Objective/KR",
		"--query \"\" --created-by-me --edited-since",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("initialization still depends on the OKR API contract %q", forbidden)
		}
	}
	if !strings.Contains(string(installSkill), "`lark-drive`、`lark-doc`") {
		t.Fatalf("install skill does not require the document capabilities needed by initialization")
	}
}

func TestBootstrapJarvisBuildsAReadBackWorldModel(t *testing.T) {
	worldModelPath := filepath.Join("..", "..", ".agents", "skills", "bootstrap-jarvis-world-model")
	paths := []string{
		"SKILL.md",
		filepath.Join("references", "modeling-guide.md"),
		filepath.Join("references", "worknote-guide.md"),
		filepath.Join("references", "ownership-map.md"),
		filepath.Join("references", "evidence-sources.md"),
	}
	var combined strings.Builder
	for _, relativePath := range paths {
		content, err := os.ReadFile(filepath.Join(worldModelPath, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		combined.Write(content)
		combined.WriteByte('\n')
	}
	contract := combined.String()
	for _, want := range []string{
		"world-model.md",
		"INSTALL_CHECKLIST.md",
		"高置信且不会覆盖存量的事实可以直接应用",
		"高影响歧义",
		"update-page",
		"list-backlinks",
		"[名称](type:id)",
		"append-fact",
		"list-facts",
		"--source initialization",
		"source_kind=initialization",
		"稳定锚点",
		"最近活动面",
		"定向扩展",
		"Group→Project、KeyMatter→Project、ManagedResource→Person/Project/Principal",
		"不生成 `approved-draft.json`、`approval.json` 或 hash 审批状态",
		"真正的业务真源始终是 M1/M2",
		"每次写入后立即使用对应 get/list/query 命令读回",
		"project_id",
		"related_group: true",
		"include_in_memory",
		"./scripts/jarvis-tools update-group --id",
		"不能回写带 `id/chat_id/name/...` 的完整对象",
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("world-model initialization contract is missing %q", want)
		}
	}
	if strings.Contains(contract, "get-context` 能看到正确 Principal、项目、人物、重点事项") {
		t.Fatal("initialize skill still claims get-context returns the whole world model")
	}
}

func TestJarvisInstallationCompletesDependenciesBeforeStartingMainService(t *testing.T) {
	installPath := filepath.Join("..", "..", ".agents", "skills", "install-jarvis")
	installSkill, err := os.ReadFile(filepath.Join(installPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	boundaries, err := os.ReadFile(filepath.Join(installPath, "references", "installation-boundaries.md"))
	if err != nil {
		t.Fatal(err)
	}
	worldModelSkill, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "bootstrap-jarvis-world-model", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	binding, err := os.ReadFile(filepath.Join(installPath, "references", "cc-connect-binding.md"))
	if err != nil {
		t.Fatal(err)
	}
	combined := string(installSkill) + "\n" + string(boundaries) + "\n" + string(binding) + "\n" + string(worldModelSkill)
	for _, want := range []string{
		"本 Skill 是从完整仓库 checkout 到最终可用的源码安装流程所有者",
		"仓库与安装运行 → 机器事实 → 全部依赖 → `validate-dependencies`",
		"./scripts/jarvis-install start",
		"run_dir/INSTALL_CHECKLIST.md",
		"依赖门通过前不得启动 CC Connect 或 Jarvis",
		"世界模型不是服务启动前置条件",
		"Qdrant 是依赖服务",
		"`install-server` 必须在调用平台服务安装脚本前再次通过依赖门",
		"install-codex",
		"install-cc-connect",
		"一个飞书 App/Bot 是身份根",
		"CC Connect 是该 Bot WebSocket 的唯一所有者",
		"install.cc-exclusive-owner",
		"validate-binding",
		"./scripts/jarvis-lark-auth check",
		"./scripts/jarvis-lark-auth begin",
		"已有 daemon 指向另一 binary/checkout",
		"初始化只负责“Jarvis 如何理解这个用户的世界”",
		"不安装或重启服务，也不配置 CC",
		"更新 `INSTALL_CHECKLIST.md` 的 E 区",
		"./scripts/jarvis-install status --run-dir <run_dir>",
		"第二次运行同一命令",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("dependency-first installation contract is missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"./scripts/jarvis-world-model start",
		"./scripts/jarvis-world-model status",
	} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("whole-project installation state still belongs to jarvis-world-model: %q", forbidden)
		}
	}
}

func TestFeishuGroupSummaryUsesSupportedThreadPagination(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(
		"..", "..", ".agents", "skills", "feishu-group-daily-summary", "references", "tool-paths.md",
	))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(raw)
	for _, want := range []string{
		"lark-cli im +threads-messages-list",
		"--page-size 50",
		"--page-all",
		"--page-limit 1000",
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("thread pagination contract is missing %q", want)
		}
	}
	if strings.Contains(contract, "--page-size 500") {
		t.Fatal("thread pagination exceeds the lark-cli 1.0.93 maximum page size")
	}
}

func TestMeetingGuidanceUsesUnifiedLarkMeetingSkill(t *testing.T) {
	paths := []string{
		filepath.Join("..", "..", "conf", "rules", "m5.md"),
		filepath.Join("..", "..", ".agents", "skills", "summarize-person-day", "references", "context-and-capabilities.md"),
		filepath.Join("..", "..", ".agents", "skills", "summarize-person-week", "references", "context-and-capabilities.md"),
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		if !strings.Contains(content, "lark-meeting") {
			t.Errorf("%s does not reference lark-meeting", path)
		}
		for _, redirectStub := range []string{"lark-vc`", "lark-minutes`", "lark-note`", "lark-vc-agent`"} {
			if strings.Contains(content, redirectStub) {
				t.Errorf("%s still references redirect stub %q", path, redirectStub)
			}
		}
	}
}

func writeSkillFixture(t *testing.T, inline bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	skillDirectory := filepath.Join(root, "example")
	if err := os.Mkdir(skillDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: example\ndescription: \"{{AGENT_NAME}} skill\"\n---\n\n# {{AGENT_NAME}} body\n\nBODY_MARKER\n"
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	config := "skills:\n  - name: example\n    enabled: true\n    stages: [extract, execute]\n"
	if inline {
		config = "skills:\n  - name: example\n    enabled: true\n    inline: true\n    stages: [extract]\n"
	}
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, configPath
}
