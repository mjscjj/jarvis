package plugin

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Manifest struct {
	ID              string
	Name            string
	Description     string
	Kind            string
	Source          string
	CollectorSkill  string
	Skills          []string
	Provider        string
	Permissions     []string
	IntervalMinutes int
	DefaultConfig   json.RawMessage
}

const (
	KindCollector  = "collector"
	KindCapability = "capability"
)

type Registry struct {
	entries map[string]Manifest
}

func NewRegistry(manifests []Manifest) (*Registry, error) {
	entries := make(map[string]Manifest, len(manifests))
	sources := make(map[string]string, len(manifests))
	skills := make(map[string]string, len(manifests))
	for _, manifest := range manifests {
		manifest.ID = strings.TrimSpace(manifest.ID)
		manifest.Name = strings.TrimSpace(manifest.Name)
		manifest.Description = strings.TrimSpace(manifest.Description)
		manifest.Kind = strings.TrimSpace(manifest.Kind)
		manifest.Source = strings.TrimSpace(manifest.Source)
		manifest.CollectorSkill = strings.TrimSpace(manifest.CollectorSkill)
		manifest.Provider = strings.TrimSpace(manifest.Provider)
		if manifest.Kind == "" {
			manifest.Kind = KindCollector
		}
		if manifest.ID == "" || manifest.Name == "" || manifest.Description == "" {
			return nil, fmt.Errorf("plugin manifest %q is incomplete", manifest.ID)
		}
		switch manifest.Kind {
		case KindCollector:
			if manifest.Source == "" || manifest.CollectorSkill == "" || manifest.Provider == "" ||
				manifest.IntervalMinutes <= 0 {
				return nil, fmt.Errorf("collector plugin manifest %q is incomplete", manifest.ID)
			}
		case KindCapability:
			if manifest.Source != "" || manifest.CollectorSkill != "" || manifest.Provider != "" ||
				manifest.IntervalMinutes != 0 || len(manifest.Skills) == 0 {
				return nil, fmt.Errorf("capability plugin manifest %q is invalid", manifest.ID)
			}
		default:
			return nil, fmt.Errorf("plugin manifest %q has unknown kind %q", manifest.ID, manifest.Kind)
		}
		if len(manifest.DefaultConfig) == 0 {
			manifest.DefaultConfig = json.RawMessage(`{}`)
		}
		defaultConfig, err := normalizeConfig(manifest.DefaultConfig)
		if err != nil {
			return nil, fmt.Errorf("plugin manifest %q default config: %w", manifest.ID, err)
		}
		manifest.DefaultConfig = defaultConfig
		if _, exists := entries[manifest.ID]; exists {
			return nil, fmt.Errorf("duplicate plugin id %q", manifest.ID)
		}
		if manifest.Source != "" {
			if owner, exists := sources[manifest.Source]; exists {
				return nil, fmt.Errorf("plugin source %q is owned by both %s and %s", manifest.Source, owner, manifest.ID)
			}
			sources[manifest.Source] = manifest.ID
		}
		ownedSkills := append([]string(nil), manifest.Skills...)
		if manifest.CollectorSkill != "" {
			ownedSkills = append(ownedSkills, manifest.CollectorSkill)
		}
		seenSkills := make(map[string]struct{}, len(ownedSkills))
		for _, name := range ownedSkills {
			name = strings.TrimSpace(name)
			if name == "" {
				return nil, fmt.Errorf("plugin manifest %q contains an empty skill", manifest.ID)
			}
			if _, duplicate := seenSkills[name]; duplicate {
				return nil, fmt.Errorf("plugin manifest %q repeats skill %q", manifest.ID, name)
			}
			seenSkills[name] = struct{}{}
			if owner, exists := skills[name]; exists {
				return nil, fmt.Errorf("plugin skill %q is owned by both %s and %s", name, owner, manifest.ID)
			}
			skills[name] = manifest.ID
		}
		manifest.Permissions = append([]string(nil), manifest.Permissions...)
		manifest.Skills = append([]string(nil), manifest.Skills...)
		manifest.DefaultConfig = append(json.RawMessage(nil), manifest.DefaultConfig...)
		entries[manifest.ID] = manifest
	}
	return &Registry{entries: entries}, nil
}

func BuiltinRegistry() (*Registry, error) {
	return NewRegistry([]Manifest{
		{
			ID: "product-management", Name: "产品流程", Kind: KindCapability,
			Description: "阅读和 Review 产品文档，汇总产品 Skills、定时任务与执行结果。",
			Skills:      []string{"product-prd-review", "product-tools"},
			Permissions: []string{"文档阅读与任务执行；Skills 由 Agent 维护，也可人工查看和编辑"},
		},
		{
			ID: "codebase", Name: "Codebase",
			Description: "采集与你相关、在配置时间范围内有更新的开放 MR 和评审请求。",
			Source:      "codebase", CollectorSkill: "codebase-clue-collector",
			Provider: "bytedcli-session", Permissions: []string{"bytedcli:codebase.read"},
			IntervalMinutes: 30, DefaultConfig: json.RawMessage(`{"lookback_days":3}`),
		},
		{
			ID: "meego", Name: "Meego",
			Description: "采集由你负责、在配置时间范围内创建且尚未完成的需求和缺陷。",
			Source:      "meego", CollectorSkill: "meego-clue-collector",
			Provider: "meego", Permissions: []string{"bytedcli:meego.read"},
			IntervalMinutes: 60, DefaultConfig: json.RawMessage(`{"lookback_days":30}`),
		},
		{
			ID: "oncall", Name: "Oncall",
			Description: "按群名规则采集你已加入的 Oncall 群和处置进展。",
			Source:      "oncall", CollectorSkill: "oncall-clue-collector",
			Provider: "lark-cli-im", Permissions: []string{"lark:im.read"},
			IntervalMinutes: 15, DefaultConfig: json.RawMessage(`{"search_terms":["oncall","值班"]}`),
		},
		{
			ID: "my-delegations", Name: "我的交办", Kind: KindCapability,
			Description: "从消息和会议中识别交办待办，独立记录交付进展，由 Task 按需核验。",
			Skills:      []string{"my-delegations-extract", "my-delegations-execute", "my-delegations-review"},
			Permissions: []string{"jarvis:tasks.read", "lark:im.read"},
		},
	})
}

func (r *Registry) Get(id string) (Manifest, bool) {
	if r == nil {
		return Manifest{}, false
	}
	manifest, ok := r.entries[strings.TrimSpace(id)]
	return manifest, ok
}

func (r *Registry) List() []Manifest {
	if r == nil {
		return nil
	}
	items := make([]Manifest, 0, len(r.entries))
	for _, manifest := range r.entries {
		manifest.Permissions = append([]string(nil), manifest.Permissions...)
		manifest.Skills = append([]string(nil), manifest.Skills...)
		manifest.DefaultConfig = append(json.RawMessage(nil), manifest.DefaultConfig...)
		items = append(items, manifest)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (r *Registry) OwnerOfSkill(name string) (string, bool) {
	name = strings.TrimSpace(name)
	for _, manifest := range r.entries {
		if manifest.CollectorSkill == name {
			return manifest.ID, true
		}
		for _, owned := range manifest.Skills {
			if owned == name {
				return manifest.ID, true
			}
		}
	}
	return "", false
}
