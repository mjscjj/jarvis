package config

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"jarvis/internal/ark"

	"gopkg.in/yaml.v3"
)

// PrincipalConfiguration is the machine-readable result of initializing the
// machine-consumed identity settings Jarvis needs before its first real run.
type PrincipalConfiguration struct {
	RuntimeConfigPath      string `json:"runtime_config_path"`
	PrincipalOpenID        string `json:"principal_open_id"`
	LarkProfile            string `json:"lark_profile"`
	GitAuthor              string `json:"git_author"`
	CardApprovalConfigured bool   `json:"card_approval_configured"`
	RelaySecretConfigured  bool   `json:"relay_secret_configured"`
	RestartRequired        bool   `json:"restart_required"`
}

// InitializationStatus projects only the machine-owned fields needed to
// decide whether Jarvis may be installed. It deliberately returns presence
// booleans instead of credential or identity values so a doctor command can be
// shared without leaking local configuration.
type InitializationStatus struct {
	BaseConfigPath            string   `json:"base_config_path"`
	RuntimeConfigPath         string   `json:"runtime_config_path"`
	RuntimeConfigExists       bool     `json:"runtime_config_exists"`
	BaseConfigMode            string   `json:"base_config_mode"`
	RuntimeConfigMode         string   `json:"runtime_config_mode,omitempty"`
	PrincipalOpenIDConfigured bool     `json:"principal_open_id_configured"`
	LarkProfileConfigured     bool     `json:"lark_profile_configured"`
	GitAuthorConfigured       bool     `json:"git_author_configured"`
	CardApprovalConfigured    bool     `json:"card_approval_configured"`
	ModelBaseURLConfigured    bool     `json:"model_base_url_configured"`
	ModelAPIKeyConfigured     bool     `json:"model_api_key_configured"`
	ModelNameConfigured       bool     `json:"model_name_configured"`
	EmbeddingModelConfigured  bool     `json:"embedding_model_configured"`
	EmbeddingDimensionsReady  bool     `json:"embedding_dimensions_configured"`
	TrackedModelAPIKeyPresent bool     `json:"tracked_model_api_key_present"`
	RuntimeBinaries           []string `json:"runtime_binaries"`
	MachineConfigurationReady bool     `json:"machine_configuration_ready"`
}

// InspectInitialization reads the strict base and optional runtime overlay
// without applying the full server-start validation. A fresh clone is
// intentionally incomplete, and the installer needs to report that state
// rather than fail before the initialization Agent can act.
func InspectInitialization(configPath string) (*InitializationStatus, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, fmt.Errorf("config path is empty")
	}
	absoluteConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path %q: %w", configPath, err)
	}
	baseRaw, err := os.ReadFile(absoluteConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", absoluteConfigPath, err)
	}
	var cfg Config
	if err := decodeKnownYAML(baseRaw, &cfg); err != nil {
		return nil, fmt.Errorf("parse base config before initialization: %w", err)
	}
	baseInfo, err := os.Stat(absoluteConfigPath)
	if err != nil {
		return nil, fmt.Errorf("stat config %q: %w", absoluteConfigPath, err)
	}
	overridePath := RuntimeOverridePath(absoluteConfigPath)
	overrideExists := false
	overrideMode := ""
	if overrideRaw, readErr := os.ReadFile(overridePath); readErr == nil {
		overrideExists = true
		if err := decodeKnownYAML(overrideRaw, &cfg); err != nil {
			return nil, fmt.Errorf("parse runtime config override %q: %w", overridePath, err)
		}
		overrideInfo, err := os.Stat(overridePath)
		if err != nil {
			return nil, fmt.Errorf("stat runtime config override %q: %w", overridePath, err)
		}
		overrideMode = fmt.Sprintf("%04o", overrideInfo.Mode().Perm())
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("read runtime config override %q: %w", overridePath, readErr)
	}

	status := &InitializationStatus{
		BaseConfigPath:            absoluteConfigPath,
		RuntimeConfigPath:         overridePath,
		RuntimeConfigExists:       overrideExists,
		BaseConfigMode:            fmt.Sprintf("%04o", baseInfo.Mode().Perm()),
		RuntimeConfigMode:         overrideMode,
		PrincipalOpenIDConfigured: strings.HasPrefix(strings.TrimSpace(cfg.Extract.PrincipalOpenID), "ou_") && len(strings.TrimSpace(cfg.Extract.PrincipalOpenID)) > len("ou_"),
		LarkProfileConfigured:     strings.TrimSpace(cfg.LarkCLI.Profile) != "",
		GitAuthorConfigured:       strings.TrimSpace(cfg.DailyDigest.GitAuthor) != "",
		CardApprovalConfigured: cfg.CardApproval.Enabled &&
			strings.TrimSpace(cfg.CardApproval.Profile) != "" &&
			strings.TrimSpace(cfg.CardApproval.PrincipalOpenID) != "" &&
			strings.TrimSpace(cfg.CardApproval.RelaySecret) != "" &&
			cfg.CardApproval.Profile == cfg.LarkCLI.Profile &&
			cfg.CardApproval.PrincipalOpenID == cfg.Extract.PrincipalOpenID,
		ModelBaseURLConfigured:    strings.TrimSpace(ark.BaseURL) != "",
		ModelAPIKeyConfigured:     strings.TrimSpace(ark.APIKey) != "",
		ModelNameConfigured:       strings.TrimSpace(cfg.Model.Model) != "",
		EmbeddingModelConfigured:  strings.TrimSpace(ark.EmbeddingModel) != "",
		EmbeddingDimensionsReady:  ark.EmbeddingDimensions > 0,
		TrackedModelAPIKeyPresent: strings.TrimSpace(ark.APIKey) != "",
		RuntimeBinaries:           initializationRuntimeBinaries(cfg),
	}
	status.MachineConfigurationReady = status.PrincipalOpenIDConfigured &&
		status.LarkProfileConfigured && status.GitAuthorConfigured && status.CardApprovalConfigured &&
		status.ModelBaseURLConfigured && status.ModelAPIKeyConfigured &&
		status.ModelNameConfigured && status.EmbeddingModelConfigured &&
		status.EmbeddingDimensionsReady
	return status, nil
}

func initializationRuntimeBinaries(cfg Config) []string {
	configured := []string{cfg.LarkCLI.Bin, cfg.Codex.Bin, cfg.Execute.Bin}
	if cfg.FactEngine.Enabled {
		configured = append(configured, cfg.FactEngine.Bin)
	}
	if cfg.Proactive.Enabled {
		configured = append(configured, cfg.Proactive.Bin)
	}
	if cfg.MeetingSweep.Enabled {
		configured = append(configured, cfg.MeetingSweep.Bin)
	}
	if cfg.MorningBrief.Enabled {
		configured = append(configured, cfg.MorningBrief.Bin)
	}
	unique := make(map[string]struct{}, len(configured))
	for _, binary := range configured {
		binary = strings.TrimSpace(binary)
		if binary != "" {
			unique[binary] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for binary := range unique {
		result = append(result, binary)
	}
	sort.Strings(result)
	return result
}

// ConfigurePrincipal writes the app-scoped principal open_id, the one selected
// lark-cli profile, its matching card-approval identity, and the principal's
// Git author pattern to the ignored runtime overlay. The relay secret is
// generated once and preserved across reruns; install-jarvis copies the same
// value into CC Connect's jarvis-codex project. It intentionally does not
// touch the tracked base config or any M1 business data.
func ConfigurePrincipal(configPath, principalOpenID, larkProfile, gitAuthor string) (*PrincipalConfiguration, error) {
	configPath = strings.TrimSpace(configPath)
	principalOpenID = strings.TrimSpace(principalOpenID)
	larkProfile = strings.TrimSpace(larkProfile)
	gitAuthor = strings.TrimSpace(gitAuthor)
	if configPath == "" {
		return nil, fmt.Errorf("config path is empty")
	}
	if !strings.HasPrefix(principalOpenID, "ou_") || len(principalOpenID) == len("ou_") {
		return nil, fmt.Errorf("principal open_id %q must start with ou_ and contain an id", principalOpenID)
	}
	if larkProfile == "" {
		return nil, fmt.Errorf("lark profile is empty")
	}
	if gitAuthor == "" {
		return nil, fmt.Errorf("git author is empty")
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
	cardApproval := mappingValue(root, "card_approval")
	relaySecret := ""
	if cardApproval != nil {
		if relayNode := mappingValue(cardApproval, "relay_secret"); relayNode != nil {
			relaySecret = strings.TrimSpace(relayNode.Value)
		}
	}
	if relaySecret == "" {
		relaySecret, err = newRelaySecret()
		if err != nil {
			return nil, err
		}
	}
	setYAMLScalar(root, "extract", "principal_open_id", principalOpenID)
	setYAMLScalar(root, "lark_cli", "profile", larkProfile)
	setYAMLScalar(root, "dailydigest", "git_author", gitAuthor)
	setYAMLBool(root, "card_approval", "enabled", true)
	setYAMLScalar(root, "card_approval", "profile", larkProfile)
	setYAMLScalar(root, "card_approval", "principal_open_id", principalOpenID)
	setYAMLScalar(root, "card_approval", "relay_secret", relaySecret)

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
		RuntimeConfigPath:      overridePath,
		PrincipalOpenID:        principalOpenID,
		LarkProfile:            larkProfile,
		GitAuthor:              gitAuthor,
		CardApprovalConfigured: true,
		RelaySecretConfigured:  true,
		RestartRequired:        true,
	}, nil
}

func newRelaySecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate CC Connect relay secret: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// RelaySecretSHA256 returns a stable comparison value without exposing the
// plaintext secret in command output.
func RelaySecretSHA256(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
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

func setYAMLBool(root *yaml.Node, section, key string, value bool) {
	setYAMLScalar(root, section, key, fmt.Sprintf("%t", value))
	valueNode := mappingValue(mappingValue(root, section), key)
	valueNode.Tag = "!!bool"
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
