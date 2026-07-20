package background

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// SeedStats reports what the one-shot initial seed created versus skipped, so a
// re-run is transparent about being idempotent.
type SeedStats struct {
	ProjectsCreated int
	ProjectsSkipped int
	TasksCreated    int
	TasksSkipped    int
	GroupsLinked    int
}

// seedProject is a project background inferred from the owner's real related
// group names (agency agent infra work). code is the idempotency key.
type seedProject struct {
	code        string
	name        string
	role        string
	status      string
	priority    uint8
	description string
	techStack   []string
}

// seedTask is one confirmed action the owner is clearly driving, materialized as
// a confirmed Todo + a pending Task snapshot so the Task board is not empty.
type seedTask struct {
	projectCode string
	actionType  string
	title       string
	detail      string
	planSteps   []string
}

// seedGroupLink attaches an already-discovered group (matched by its exact
// Feishu name) to a seed project and marks whether it is a key group.
type seedGroupLink struct {
	groupName   string
	projectCode string
	keyGroup    bool
}

// The seed content below is inferred from the owner's real related group names
// (see feishu_group where related_group=1): agency ("公会") agent infrastructure,
// runtime (codex/openclaw), skill governance, and the self-built activity AI bot.
var seedProjects = []seedProject{
	{code: "agency-agent-infra", name: "公会 Agent 基建", role: "participant", status: "active", priority: 1,
		description: "公会业务的 Agent 基础设施攻坚：runtime、skill、沙箱与自进化方向的整体建设。",
		techStack:   []string{"Go", "codex", "openclaw"}},
	{code: "agent-runtime", name: "Agent Runtime", role: "participant", status: "active", priority: 1,
		description: "对下 Agent runtime 攻坚，codex 方案落地与评审，openclaw runtime 讨论。",
		techStack:   []string{"codex", "openclaw"}},
	{code: "skill-governance", name: "Skill 管理与治理", role: "participant", status: "active", priority: 2,
		description: "Skill 管理研发、治理 AI Skill、白名单用户开放与后台建设。",
		techStack:   []string{"Go"}},
	{code: "backstage-ai-bot", name: "自建活动 AI 助手 (Backstage/Bax)", role: "participant", status: "active", priority: 2,
		description: "公会经营&主播场景的 Backstage 自建活动 AI 助手 MVP，外部权限与安全问题跟进。",
		techStack:   []string{"Go"}},
}

var seedTasks = []seedTask{
	{projectCode: "agent-runtime", actionType: "investigate", title: "推进 codex runtime 方案 review",
		detail: "整理对下 runtime 的 codex 方案要点，组织技术评审并沉淀结论。",
		planSteps: []string{"梳理 codex runtime 现状与目标", "组织方案 review", "记录评审结论与后续项"}},
	{projectCode: "agency-agent-infra", actionType: "investigate", title: "梳理 Agent 自进化命题方向",
		detail: "对齐 Agent 自进化的内部方向，输出可评审的路径草案。",
		planSteps: []string{"收集自进化命题输入", "输出方向草案", "内部 review 对齐"}},
	{projectCode: "skill-governance", actionType: "manual_followup", title: "推进 skills 白名单用户开放",
		detail: "支持 skills 白名单用户可用，明确开放范围与后台配置。",
		planSteps: []string{"确认白名单范围", "后台开放配置", "验证可用性"}},
	{projectCode: "backstage-ai-bot", actionType: "manual_followup", title: "跟进自建活动 AI Bot 外部权限安全问题",
		detail: "自建活动 AI Bot 外部权限开启涉及安全问题，需推动讨论并给出方案。",
		planSteps: []string{"梳理外部权限风险点", "组织安全讨论", "输出权限开启方案"}},
}

var seedGroupLinks = []seedGroupLink{
	{groupName: "【研发】公会Agent基建攻坚群", projectCode: "agency-agent-infra", keyGroup: true},
	{groupName: "公会AI突击", projectCode: "agency-agent-infra", keyGroup: true},
	{groupName: "Agent runtime 攻坚小队", projectCode: "agent-runtime", keyGroup: true},
	{groupName: "openclaw runtime讨论", projectCode: "agent-runtime", keyGroup: false},
	{groupName: "agent runtine codex方案review", projectCode: "agent-runtime", keyGroup: false},
	{groupName: "对下runtime", projectCode: "agent-runtime", keyGroup: false},
	{groupName: "Skill管理研发群", projectCode: "skill-governance", keyGroup: true},
	{groupName: "[治理 AI Skill] 产研讨论群", projectCode: "skill-governance", keyGroup: false},
	{groupName: "skills后台", projectCode: "skill-governance", keyGroup: false},
	{groupName: "支持skills白名单用户可用", projectCode: "skill-governance", keyGroup: false},
	{groupName: "自建活动AI助手产研沟通群", projectCode: "backstage-ai-bot", keyGroup: true},
	{groupName: "自建活动AI Bot外部权限开启安全问题讨论", projectCode: "backstage-ai-bot", keyGroup: false},
	{groupName: "公会Agent自建方案技术评审", projectCode: "backstage-ai-bot", keyGroup: false},
}

// Seed writes the initial project/task/group-link backgrounds inferred from the
// owner's real related groups. It is a one-shot idempotent operation: rows keyed
// by a stable identifier are skipped on re-run. Runs in a single transaction and
// fails fast on any error (no partial seed).
func Seed(ctx context.Context, db *gorm.DB) (*SeedStats, error) {
	if db == nil {
		return nil, fmt.Errorf("seed db is nil")
	}
	stats := &SeedStats{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		projectIDByCode := make(map[string]uint64, len(seedProjects))
		for _, sp := range seedProjects {
			id, created, err := seedOneProject(tx, sp)
			if err != nil {
				return err
			}
			projectIDByCode[sp.code] = id
			if created {
				stats.ProjectsCreated++
			} else {
				stats.ProjectsSkipped++
			}
		}
		for _, st := range seedTasks {
			projectID, ok := projectIDByCode[st.projectCode]
			if !ok {
				return fmt.Errorf("seed task %q references unknown project code %q", st.title, st.projectCode)
			}
			created, err := seedOneTask(tx, st, projectID)
			if err != nil {
				return err
			}
			if created {
				stats.TasksCreated++
			} else {
				stats.TasksSkipped++
			}
		}
		for _, link := range seedGroupLinks {
			projectID, ok := projectIDByCode[link.projectCode]
			if !ok {
				return fmt.Errorf("seed group link %q references unknown project code %q", link.groupName, link.projectCode)
			}
			linked, err := seedOneGroupLink(tx, link, projectID)
			if err != nil {
				return err
			}
			if linked {
				stats.GroupsLinked++
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("seed backgrounds: %w", err)
	}
	return stats, nil
}

func seedOneProject(tx *gorm.DB, sp seedProject) (uint64, bool, error) {
	var existing domain.Project
	found := tx.Where("code = ?", sp.code).Limit(1).Find(&existing)
	if found.Error != nil {
		return 0, false, fmt.Errorf("lookup seed project %q: %w", sp.code, found.Error)
	}
	if found.RowsAffected == 1 {
		return existing.ID, false, nil
	}
	techStack, err := seedJSON(sp.techStack)
	if err != nil {
		return 0, false, fmt.Errorf("encode seed tech_stack %q: %w", sp.code, err)
	}
	code := sp.code
	description := sp.description
	project := domain.Project{
		Code: &code, Name: sp.name, Role: sp.role, Status: sp.status,
		Priority: sp.priority, Description: &description, TechStack: techStack,
	}
	if err := tx.Create(&project).Error; err != nil {
		return 0, false, fmt.Errorf("create seed project %q: %w", sp.code, err)
	}
	return project.ID, true, nil
}

func seedOneTask(tx *gorm.DB, st seedTask, projectID uint64) (bool, error) {
	fingerprint := seedFingerprint("task", st.projectCode, st.title)
	var existing domain.Todo
	found := tx.Where("dedup_fingerprint = ?", fingerprint).Limit(1).Find(&existing)
	if found.Error != nil {
		return false, fmt.Errorf("lookup seed todo %q: %w", st.title, found.Error)
	}
	if found.RowsAffected == 1 {
		return false, nil
	}
	now := time.Now()
	openQuestions, err := seedJSON([]string{})
	if err != nil {
		return false, fmt.Errorf("encode seed open questions %q: %w", st.title, err)
	}
	sources, err := seedJSON([]string{})
	if err != nil {
		return false, fmt.Errorf("encode seed sources %q: %w", st.title, err)
	}
	todo := domain.Todo{
		Title: st.title, Description: st.detail, ActionType: st.actionType,
		Target: st.title, Context: "(seed)", OpenQuestions: openQuestions,
		CommitmentStrength: "firm",
		SourceMessageIDs:   sources, SourceQuote: "(seed)",
		ProjectID: &projectID, Status: "confirmed",
		DedupFingerprint: fingerprint, ExtractionModel: "seed", PromptVersion: "seed",
		FirstSeenAt: now, LastEvidenceAt: now,
	}
	if err := tx.Create(&todo).Error; err != nil {
		return false, fmt.Errorf("create seed todo %q: %w", st.title, err)
	}
	background, err := seedJSON(map[string]any{
		"project_code": st.projectCode, "note": "seed inferred from related groups",
	})
	if err != nil {
		return false, fmt.Errorf("encode seed background %q: %w", st.title, err)
	}
	plan, err := seedJSON(map[string]any{"steps": st.planSteps})
	if err != nil {
		return false, fmt.Errorf("encode seed plan %q: %w", st.title, err)
	}
	task := domain.Task{
		TodoID: todo.ID, Title: st.title, ActionType: st.actionType,
		Background: background, Plan: plan,
		ConfirmedBy: "system", ConfirmedAt: now, ActionHash: fingerprint,
		Status: "pending", AutonomyMode: "copilot", ProjectID: &projectID,
	}
	if err := tx.Create(&task).Error; err != nil {
		return false, fmt.Errorf("create seed task %q: %w", st.title, err)
	}
	return true, nil
}

func seedOneGroupLink(tx *gorm.DB, link seedGroupLink, projectID uint64) (bool, error) {
	result := tx.Model(&domain.Group{}).
		Where("name = ?", link.groupName).
		Select("project_id", "related_group", "is_key_group").
		Updates(map[string]any{"project_id": projectID, "related_group": true, "is_key_group": link.keyGroup})
	if result.Error != nil {
		return false, fmt.Errorf("link seed group %q: %w", link.groupName, result.Error)
	}
	return result.RowsAffected > 0, nil
}

func seedFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte("seed:" + fmt.Sprint(parts)))
	return hex.EncodeToString(sum[:])
}

func seedJSON(value any) (datatypes.JSON, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(raw), nil
}
