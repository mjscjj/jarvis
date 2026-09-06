// Package appmodule owns Jarvis's built-in optional feature modules.
//
// Module code and metadata stay code-owned. conf/modules.yaml only decides
// whether a known module is visible in the product, so disabling a module does
// not fork or delete its world-model data.
package appmodule

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"jarvis/internal/fileconfig"

	"gopkg.in/yaml.v3"
)

var (
	ErrInvalidInput = errors.New("invalid app module input")
	ErrNotFound     = errors.New("app module not found")
)

type definition struct {
	Name        string
	Description string
	Requires    []string
}

var definitions = map[string]definition{
	"okr": {
		Name:        "OKR 插件",
		Description: "通用目标、KR、负责人、核心指标、拆解、周次与正式进展",
	},
	"agency-okr": {
		Name:        "OKR",
		Description: "Agency 打标、Plan、Review、周报、催填与业务自动化",
		Requires:    []string{"okr"},
	},
}

type Input struct {
	IsEnabled *bool `json:"is_enabled"`
}

type View struct {
	Key               string   `json:"key"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	IsEnabled         bool     `json:"is_enabled"`
	ConfiguredEnabled bool     `json:"configured_enabled"`
	RestartRequired   bool     `json:"restart_required"`
	Requires          []string `json:"requires"`
}

type Service struct {
	configPath string
	runtime    map[string]bool
	mu         sync.Mutex
}

type configFile struct {
	Modules []setting `yaml:"modules"`
}

type setting struct {
	Key       string `yaml:"key"`
	IsEnabled bool   `yaml:"enabled"`
}

func NewService(configPath string) (*Service, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, fmt.Errorf("app module config path is empty")
	}
	service := &Service{configPath: configPath}
	cfg, err := service.loadConfig()
	if err != nil {
		return nil, fmt.Errorf("validate app module config: %w", err)
	}
	views, err := join(cfg)
	if err != nil {
		return nil, fmt.Errorf("validate app module config: %w", err)
	}
	service.runtime = make(map[string]bool, len(views))
	for _, view := range views {
		service.runtime[view.Key] = view.IsEnabled
	}
	return service, nil
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	views, err := join(cfg)
	if err != nil {
		return nil, err
	}
	s.decorate(views)
	return views, nil
}

// Enabled reports whether a known module is active in the current product
// runtime. Callers use this as the single gate for routes, Skills, migrations
// and schedulers; hiding navigation alone is not a module boundary.
func (s *Service) Enabled(ctx context.Context, key string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	key = strings.TrimSpace(key)
	if _, ok := definitions[key]; !ok {
		return false, fmt.Errorf("%w: key=%s", ErrNotFound, key)
	}
	enabled, ok := s.runtime[key]
	if !ok {
		return false, fmt.Errorf("%w: key=%s", ErrNotFound, key)
	}
	return enabled, nil
}

func (s *Service) Update(ctx context.Context, key string, input Input) (*View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if _, ok := definitions[key]; !ok {
		return nil, fmt.Errorf("%w: key=%s", ErrNotFound, key)
	}
	if input.IsEnabled == nil {
		return nil, fmt.Errorf("%w: is_enabled is required", ErrInvalidInput)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	if _, err := join(cfg); err != nil {
		return nil, err
	}
	found := false
	for i := range cfg.Modules {
		if cfg.Modules[i].Key == key {
			cfg.Modules[i].IsEnabled = *input.IsEnabled
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: key=%s", ErrNotFound, key)
	}
	if err := validateDependencies(cfg); err != nil {
		return nil, err
	}
	sort.Slice(cfg.Modules, func(i, j int) bool { return cfg.Modules[i].Key < cfg.Modules[j].Key })
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode app module config: %w", err)
	}
	if err := fileconfig.WriteAtomic(s.configPath, encoded); err != nil {
		return nil, err
	}
	views, err := join(cfg)
	if err != nil {
		return nil, err
	}
	s.decorate(views)
	for i := range views {
		if views[i].Key == key {
			return &views[i], nil
		}
	}
	return nil, fmt.Errorf("%w: key=%s", ErrNotFound, key)
}

func (s *Service) decorate(views []View) {
	for i := range views {
		configured := views[i].IsEnabled
		effective := s.runtime[views[i].Key]
		views[i].ConfiguredEnabled = configured
		views[i].IsEnabled = effective
		views[i].RestartRequired = configured != effective
	}
}

func (s *Service) loadConfig() (configFile, error) {
	raw, err := fileconfig.Read(s.configPath)
	if err != nil {
		return configFile{}, err
	}
	var cfg configFile
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return configFile{}, fmt.Errorf("decode app module config %s: %w", s.configPath, err)
	}
	seen := make(map[string]struct{}, len(cfg.Modules))
	for i := range cfg.Modules {
		cfg.Modules[i].Key = strings.TrimSpace(cfg.Modules[i].Key)
		if _, exists := definitions[cfg.Modules[i].Key]; !exists {
			return configFile{}, fmt.Errorf("%w: unknown configured module %q", ErrInvalidInput, cfg.Modules[i].Key)
		}
		if _, exists := seen[cfg.Modules[i].Key]; exists {
			return configFile{}, fmt.Errorf("%w: duplicate configured module %q", ErrInvalidInput, cfg.Modules[i].Key)
		}
		seen[cfg.Modules[i].Key] = struct{}{}
	}
	return cfg, nil
}

func join(cfg configFile) ([]View, error) {
	settings := make(map[string]setting, len(cfg.Modules))
	for _, item := range cfg.Modules {
		settings[item.Key] = item
	}
	for key := range definitions {
		if _, ok := settings[key]; !ok {
			return nil, fmt.Errorf("%w: built-in module %q is missing from config", ErrInvalidInput, key)
		}
	}
	if err := validateDependencies(cfg); err != nil {
		return nil, err
	}
	views := make([]View, 0, len(definitions))
	for key, item := range definitions {
		requires := make([]string, len(item.Requires))
		copy(requires, item.Requires)
		views = append(views, View{
			Key: key, Name: item.Name, Description: item.Description,
			IsEnabled: settings[key].IsEnabled, Requires: requires,
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Key < views[j].Key })
	return views, nil
}

func validateDependencies(cfg configFile) error {
	settings := make(map[string]bool, len(cfg.Modules))
	for _, item := range cfg.Modules {
		settings[item.Key] = item.IsEnabled
	}
	for key, item := range definitions {
		if !settings[key] {
			continue
		}
		for _, dependency := range item.Requires {
			if !settings[dependency] {
				return fmt.Errorf("%w: module %q requires enabled module %q", ErrInvalidInput, key, dependency)
			}
		}
	}
	return nil
}
