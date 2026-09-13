// Package agentenv prepares the process environment inherited by every Agent.
package agentenv

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalAPIBase turns a listening address into the URL used by local Agents.
// Wildcard listeners use the corresponding loopback address; no other instance
// is probed or substituted when the configured listener is unavailable.
func LocalAPIBase(address string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return "", fmt.Errorf("invalid Agent API address %q: %w", address, err)
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return "", fmt.Errorf("Agent API address %q requires a port between 1 and 65535", address)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.FormatUint(portNumber, 10))}).String(), nil
}

// ConfigureTools sets the canonical scripts, API connection and business
// timezone inherited by every Agent subprocess. Call it once after resolving
// runtime config and listen-address overrides, before constructing any runner.
func ConfigureTools(runtimeRoot, address, timezone string) error {
	root := strings.TrimSpace(runtimeRoot)
	if root == "" {
		return fmt.Errorf("agent tool runtime root is empty")
	}
	scriptsDir, err := filepath.Abs(filepath.Join(root, "scripts"))
	if err != nil {
		return fmt.Errorf("resolve agent scripts directory: %w", err)
	}
	toolPath := filepath.Join(scriptsDir, "jarvis-tools")
	info, err := os.Stat(toolPath)
	if err != nil {
		return fmt.Errorf("stat agent tool %q: %w", toolPath, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("agent tool is not executable: %s", toolPath)
	}

	apiBase, err := LocalAPIBase(address)
	if err != nil {
		return err
	}
	if timezone == "" {
		return fmt.Errorf("Agent timezone is empty")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid Agent timezone %q: %w", timezone, err)
	}

	parts := []string{scriptsDir}
	for _, part := range filepath.SplitList(os.Getenv("PATH")) {
		if part != "" && filepath.Clean(part) != scriptsDir {
			parts = append(parts, part)
		}
	}
	for _, setting := range []struct{ key, value string }{
		{"PATH", strings.Join(parts, string(os.PathListSeparator))},
		{"JARVIS_API_BASE", apiBase},
		{"JARVIS_TIMEZONE", timezone},
	} {
		if err := os.Setenv(setting.key, setting.value); err != nil {
			return fmt.Errorf("configure Agent %s: %w", setting.key, err)
		}
	}
	return nil
}
