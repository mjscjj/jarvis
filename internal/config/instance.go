package config

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// Instance is the local process identity used by launchd and development tools.
// The label is stable when the port changes and distinct for each config file.
type Instance struct {
	ConfigPath   string   `json:"config_path"`
	APIBase      string   `json:"api_base"`
	LaunchdLabel string   `json:"launchd_label"`
	LogFiles     []string `json:"log_files"`
}

func (s ServerConfig) APIBase() (string, error) {
	return apiBaseForAddr("server.addr", s.Addr)
}

func apiBaseForAddr(name, addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid %s: %w", name, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid %s port %q", name, port)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

func InspectInstance(configPath string) (*Instance, error) {
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	cfg, err := readConfig(absolute)
	if err != nil {
		return nil, err
	}
	apiBase, err := cfg.Server.APIBase()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(absolute))
	label := fmt.Sprintf("com.bytedance.jarvis.server.%x", digest[:8])
	return &Instance{
		ConfigPath: absolute, APIBase: apiBase, LogFiles: cfg.Server.LogFiles, LaunchdLabel: label,
	}, nil
}

// ExportToolEnvironment runs once, before any server workers start. Child
// agents keep the owning instance even after switching repositories or using a
// globally installed jarvis-tools. An inherited parent URL cannot redirect it.
func (c *Config) ExportToolEnvironment(configPath, repoRoot string) error {
	apiBase, err := c.Server.APIBase()
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return err
	}
	for key, value := range map[string]string{
		"JARVIS_CONFIG":   absolute,
		"JARVIS_API_BASE": apiBase,
		"PATH":            filepath.Join(root, "scripts") + string(os.PathListSeparator) + os.Getenv("PATH"),
	} {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("export %s: %w", key, err)
		}
	}
	return nil
}
