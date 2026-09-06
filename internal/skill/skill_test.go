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
	meta, err := parseMetadata([]byte("---\nname: feishu-send-message\ndescription: 发送飞书消息\n---\n\n# 正文\n"))
	if err != nil {
		t.Fatalf("parseMetadata() error = %v", err)
	}
	if meta.Name != "feishu-send-message" || meta.Description != "发送飞书消息" {
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
	if _, err := normalizeStages([]string{"M6"}); err == nil {
		t.Fatal("normalizeStages(unknown) must fail")
	}
}

func TestServiceReadsAndUpdatesYAMLConfiguration(t *testing.T) {
	root := t.TempDir()
	skillDirectory := filepath.Join(root, "feishu-send-message")
	if err := os.Mkdir(skillDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	skillText := "---\nname: feishu-send-message\ndescription: 发送飞书消息\n---\n\n# 正文\n"
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte(skillText), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: feishu-send-message\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	items, err := service.List(t.Context())
	if err != nil || len(items) != 1 || items[0].Name != "feishu-send-message" {
		t.Fatalf("List() = %#v err=%v", items, err)
	}
	enabled := false
	updated, err := service.Update(t.Context(), "feishu-send-message", Input{
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

func TestServiceAvailabilityGateHidesPluginSkill(t *testing.T) {
	root := t.TempDir()
	skillDirectory := filepath.Join(root, "plugin-collector")
	if err := os.Mkdir(skillDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte(
		"---\nname: plugin-collector\ndescription: collect plugin clues\n---\n\n# Collector\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte(
		"skills:\n  - name: plugin-collector\n    enabled: true\n    stages: [execute]\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	service.SetAvailability(testAvailability{"plugin-collector": false})
	items, err := service.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].IsEnabled || items[0].IsAvailable {
		t.Fatalf("items = %#v", items)
	}
	if _, err := service.Content(t.Context(), "plugin-collector"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Content() error = %v, want ErrNotFound", err)
	}
}

func TestRenderingServiceRendersCatalogAndContentWithoutChangingSource(t *testing.T) {
	root := t.TempDir()
	skillDirectory := filepath.Join(root, "example-skill")
	if err := os.Mkdir(skillDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	skillText := "---\nname: example-skill\ndescription: \"{{AGENT_NAME}} 可用能力\"\n---\n\n# {{AGENT_NAME}} 正文\n"
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte(skillText), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "skills.yaml")
	if err := os.WriteFile(configPath, []byte("skills:\n  - name: example-skill\n    enabled: true\n    stages: [execute]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	content, err := service.Content(t.Context(), "example-skill")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(catalog, "小贾 可用能力") || !strings.Contains(content.Content, "# 小贾 正文") {
		t.Fatalf("catalog=%q content=%q", catalog, content.Content)
	}
	raw, err := source.Content(t.Context(), "example-skill")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw.Content, "{{AGENT_NAME}}") {
		t.Fatalf("source content was mutated: %q", raw.Content)
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
		"所有 M5 普通业务消息都使用本 Skill",
		"不执行本 Skill 的任何写命令",
		"jarvis-config show-principal",
		"不能改读 Task 仓库里的同名文件",
		"lark-cli auth status --json --verify",
		"lark-cli 当前默认身份",
		"user `openId` 与 principal `open_id` 完全相同",
		"+chat-search",
		"--page-token",
		"+chat-members-list",
		"--page-all --page-limit 0",
		"多个候选、成员不完整或用途不确定时停止",
		"不要自动创建群、改用 user 身份或更换目标",
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
		"scripts/agency-okr-tools board",
		"scripts/agency-okr-tools record-meego-observation",
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
		"本 Skill 是从完整仓库 checkout 到最终可用的安装流程所有者",
		"仓库与安装运行 → 机器事实 → 全部依赖 → `validate-dependencies`",
		"./scripts/jarvis-install start",
		"run_dir/INSTALL_CHECKLIST.md",
		"依赖门通过前不得启动 CC Connect 或 Jarvis",
		"世界模型不是服务启动前置条件",
		"Qdrant 是依赖服务",
		"`install-server` 必须在调用平台服务安装脚本前再次通过依赖门",
		"install-cc-connect",
		"一个飞书 App/Bot 是身份根",
		"CC Connect 是该 Bot WebSocket 的唯一所有者",
		"validate-binding",
		"已有 daemon 指向另一 binary/checkout",
		"初始化只负责“Jarvis 如何理解这个用户的世界”",
		"不安装或重启 daemon，也不配置 CC",
		"只更新清单 E 区",
		"./scripts/jarvis-install status --run-dir <run_dir>",
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
