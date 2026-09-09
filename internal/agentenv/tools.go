// Package agentenv prepares the process environment inherited by every Agent.
package agentenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigureTools makes the runtime's canonical scripts available to every
// Agent subprocess. Call it once during server startup before constructing any
// runner.
func ConfigureTools(runtimeRoot string) error {
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

	parts := []string{scriptsDir}
	for _, part := range filepath.SplitList(os.Getenv("PATH")) {
		if part != "" && filepath.Clean(part) != scriptsDir {
			parts = append(parts, part)
		}
	}
	if err := os.Setenv("PATH", strings.Join(parts, string(os.PathListSeparator))); err != nil {
		return fmt.Errorf("configure agent tool PATH: %w", err)
	}
	return nil
}
