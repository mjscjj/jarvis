package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MigrateRetiredConfig removes or moves retired settings before deployment's
// strict config read. It does not restore support for retired settings.
func MigrateRetiredConfig(configPath string) error {
	for _, path := range []string{configPath, RuntimeOverridePath(configPath)} {
		if _, err := os.Stat(path); os.IsNotExist(err) && path != configPath {
			continue
		} else if err != nil {
			return err
		}
		document, err := readRuntimeOverrideDocument(path)
		if err != nil {
			return err
		}
		root := document.Content[0]
		section := mappingValue(root, "chat")
		changed := false
		for _, key := range []string{"bin", "addr", "fast_mode", "history_dir"} {
			if mappingValue(section, key) != nil {
				removeYAMLMappingKey(root, "chat", key)
				changed = true
			}
		}
		server := mappingValue(root, "server")
		if old := mappingValue(server, "public_url"); old != nil {
			if current := mappingValue(server, "public_base_url"); current == nil || current.Value == "" {
				setYAMLScalar(root, "server", "public_base_url", old.Value)
			}
			removeYAMLMappingKey(root, "server", "public_url")
			changed = true
		}
		if !changed {
			continue
		}
		raw, err := yaml.Marshal(document)
		if err != nil {
			return err
		}
		var cfg Config
		if err := decodeKnownYAML(raw, &cfg); err != nil {
			return fmt.Errorf("validate config after migrating retired settings: %w", err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return fmt.Errorf("migrate retired settings in %q: %w", path, err)
		}
	}
	return nil
}
