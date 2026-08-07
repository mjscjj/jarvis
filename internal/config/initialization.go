package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PrincipalConfiguration is the machine-readable result of initializing the
// two app-scoped identity settings Jarvis needs before its first real run.
type PrincipalConfiguration struct {
	RuntimeConfigPath string `json:"runtime_config_path"`
	PrincipalOpenID   string `json:"principal_open_id"`
	LarkProfile       string `json:"lark_profile"`
	RestartRequired   bool   `json:"restart_required"`
}

// ConfigurePrincipal writes the app-scoped principal open_id and lark-cli
// profile to the ignored runtime overlay. It intentionally does not touch the
// tracked base config or any M1 business data. Existing unrelated overlay keys
// are preserved so a setup run cannot erase local secrets or runtime tuning.
func ConfigurePrincipal(configPath, principalOpenID, larkProfile string) (*PrincipalConfiguration, error) {
	configPath = strings.TrimSpace(configPath)
	principalOpenID = strings.TrimSpace(principalOpenID)
	larkProfile = strings.TrimSpace(larkProfile)
	if configPath == "" {
		return nil, fmt.Errorf("config path is empty")
	}
	if !strings.HasPrefix(principalOpenID, "ou_") || len(principalOpenID) == len("ou_") {
		return nil, fmt.Errorf("principal open_id %q must start with ou_ and contain an id", principalOpenID)
	}
	if larkProfile == "" {
		return nil, fmt.Errorf("lark profile is empty")
	}

	absoluteConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path %q: %w", configPath, err)
	}
	baseRaw, err := os.ReadFile(absoluteConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", absoluteConfigPath, err)
	}
	overridePath := RuntimeOverridePath(absoluteConfigPath)
	document, err := readRuntimeOverrideDocument(overridePath)
	if err != nil {
		return nil, err
	}
	root := document.Content[0]
	setYAMLScalar(root, "extract", "principal_open_id", principalOpenID)
	setYAMLScalar(root, "lark_cli", "profile", larkProfile)

	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode runtime config override: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close runtime config encoder: %w", err)
	}
	if err := validateMergedConfig(baseRaw, encoded.Bytes()); err != nil {
		return nil, err
	}
	if err := writeRuntimeOverrideBytes(overridePath, encoded.Bytes()); err != nil {
		return nil, err
	}
	return &PrincipalConfiguration{
		RuntimeConfigPath: overridePath,
		PrincipalOpenID:   principalOpenID,
		LarkProfile:       larkProfile,
		RestartRequired:   true,
	}, nil
}

func readRuntimeOverrideDocument(path string) (*yaml.Node, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read runtime config override %q: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode runtime config override %q: %w", path, err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("runtime config override %q must contain one YAML mapping", path)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("runtime config override %q must contain exactly one YAML document", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode trailing runtime config override %q: %w", path, err)
	}
	return &document, nil
}

func setYAMLScalar(root *yaml.Node, section, key, value string) {
	sectionNode := mappingValue(root, section)
	if sectionNode == nil {
		sectionNode = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: section},
			sectionNode,
		)
	}
	valueNode := mappingValue(sectionNode, key)
	if valueNode == nil {
		sectionNode.Content = append(sectionNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
		)
		return
	}
	valueNode.Kind = yaml.ScalarNode
	valueNode.Tag = "!!str"
	valueNode.Value = value
	valueNode.Content = nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func validateMergedConfig(baseRaw, overrideRaw []byte) error {
	var cfg Config
	if err := decodeKnownYAML(baseRaw, &cfg); err != nil {
		return fmt.Errorf("parse base config before initialization: %w", err)
	}
	if err := decodeKnownYAML(overrideRaw, &cfg); err != nil {
		return fmt.Errorf("parse runtime config override after initialization: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return fmt.Errorf("validate config after initialization: %w", err)
	}
	return nil
}

func writeRuntimeOverrideBytes(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".config.runtime-initialization-*.yaml")
	if err != nil {
		return fmt.Errorf("create runtime config override temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("chmod runtime config override temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write runtime config override temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close runtime config override temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace runtime config override %q: %w", path, err)
	}
	return nil
}
