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
	Source          string
	CollectorSkill  string
	Provider        string
	Permissions     []string
	IntervalMinutes int
	DefaultConfig   json.RawMessage
}

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
		manifest.Source = strings.TrimSpace(manifest.Source)
		manifest.CollectorSkill = strings.TrimSpace(manifest.CollectorSkill)
		manifest.Provider = strings.TrimSpace(manifest.Provider)
		if manifest.ID == "" || manifest.Name == "" || manifest.Description == "" ||
			manifest.Source == "" || manifest.CollectorSkill == "" || manifest.Provider == "" ||
			manifest.IntervalMinutes <= 0 {
			return nil, fmt.Errorf("plugin manifest %q is incomplete", manifest.ID)
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
		if owner, exists := sources[manifest.Source]; exists {
			return nil, fmt.Errorf("plugin source %q is owned by both %s and %s", manifest.Source, owner, manifest.ID)
		}
		if owner, exists := skills[manifest.CollectorSkill]; exists {
			return nil, fmt.Errorf("plugin skill %q is owned by both %s and %s", manifest.CollectorSkill, owner, manifest.ID)
		}
		manifest.Permissions = append([]string(nil), manifest.Permissions...)
		manifest.DefaultConfig = append(json.RawMessage(nil), manifest.DefaultConfig...)
		entries[manifest.ID] = manifest
		sources[manifest.Source] = manifest.ID
		skills[manifest.CollectorSkill] = manifest.ID
	}
	return &Registry{entries: entries}, nil
}

func BuiltinRegistry() (*Registry, error) {
	return NewRegistry([]Manifest{
		{
			ID: "codebase", Name: "Codebase",
			Description: "采集与你相关的开放 MR、评审请求和代码变更。",
			Source:      "codebase", CollectorSkill: "codebase-clue-collector",
			Provider: "bytedcli-session", Permissions: []string{"bytedcli:codebase.read"},
			IntervalMinutes: 30,
		},
		{
			ID: "meego", Name: "Meego",
			Description: "采集由你负责且尚未完成的需求、缺陷和状态变化。",
			Source:      "meego", CollectorSkill: "meego-clue-collector",
			Provider: "meego", Permissions: []string{"bytedcli:meego.read"},
			IntervalMinutes: 60,
		},
		{
			ID: "oncall", Name: "Oncall",
			Description: "按群名规则采集你已加入的 Oncall 群和处置进展。",
			Source:      "oncall", CollectorSkill: "oncall-clue-collector",
			Provider: "lark-cli-im", Permissions: []string{"lark:im.read"},
			IntervalMinutes: 15, DefaultConfig: json.RawMessage(`{"search_terms":["oncall","值班"]}`),
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
		manifest.DefaultConfig = append(json.RawMessage(nil), manifest.DefaultConfig...)
		items = append(items, manifest)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (r *Registry) OwnerOfSkill(name string) (string, bool) {
	for _, manifest := range r.entries {
		if manifest.CollectorSkill == name {
			return manifest.ID, true
		}
	}
	return "", false
}
