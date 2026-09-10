package onboarding

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
	"jarvis/internal/config"
)

// Read only the identity saved by ConfigurePrincipal. Full config.Load cannot
// be used before setup: validation requires the fields being configured.
func (s *Service) savedIdentity() (name, openID string, err error) {
	raw, err := os.ReadFile(config.RuntimeOverridePath(s.options.ConfigPath))
	if errors.Is(err, os.ErrNotExist) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	var saved struct {
		Identity struct {
			DisplayName string `yaml:"display_name"`
		} `yaml:"identity"`
		Extract struct {
			PrincipalOpenID string `yaml:"principal_open_id"`
		} `yaml:"extract"`
	}
	if err := yaml.Unmarshal(raw, &saved); err != nil {
		return "", "", err
	}
	// The desktop bootstrap overlay has a placeholder open_id but no selected
	// assistant name. It is not an identity saved by ConfigurePrincipal.
	if saved.Identity.DisplayName == "" {
		return "", "", nil
	}
	return saved.Identity.DisplayName, saved.Extract.PrincipalOpenID, nil
}
