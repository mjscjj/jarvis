package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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

func TestRepositoryFeishuApprovalCardIsOwnedByServer(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "feishu-send-message", "SKILL.md"))
	if err != nil {
		t.Fatalf("read repository Feishu message skill: %v", err)
	}
	skill := string(content)
	for _, want := range []string{
		"审批通知不由这个 Skill 发送",
		"不要用本 Skill 发送审批卡片或纯文字提醒",
		"先持久化提案",
		"绑定当前 Task version",
	} {
		if !strings.Contains(skill, want) {
			t.Fatalf("Feishu message skill missing approval-card contract %q:\n%s", want, skill)
		}
	}
	for _, obsolete := range []string{
		`"action": "jarvis_approval"`,
		`--msg-type interactive`,
		"卡片连续发送失败就退回",
	} {
		if strings.Contains(skill, obsolete) {
			t.Fatalf("Feishu message skill still contains obsolete approval-card rule %q:\n%s", obsolete, skill)
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
		"create-relation",
		"list-relations",
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
		"`install-server` 必须在调用 `install-launchd.sh` 前再次通过依赖门",
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
