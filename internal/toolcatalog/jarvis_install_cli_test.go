package toolcatalog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJarvisInstallIsSkillLocalAndAgentDriven(t *testing.T) {
	help, err := runJarvisInstall(t, nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"doctor",
		"install-qdrant",
		"install-server",
		"validate",
		"The calling",
		"Agent decides",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("jarvis-install help missing %q:\n%s", want, help)
		}
	}

	toolsHelp, err := runJarvisTools(t, "", nil, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"install-qdrant", "install-server", "initialization-status"} {
		if strings.Contains(toolsHelp, forbidden) {
			t.Fatalf("jarvis-tools exposes installation-only command %q:\n%s", forbidden, toolsHelp)
		}
	}
}

func TestJarvisInstallRefusesToReplaceLoadedServer(t *testing.T) {
	binDir := t.TempDir()
	for _, commandName := range []string{"curl", "go", "npm", "lark-cli", "traex"} {
		writeExecutable(t, filepath.Join(binDir, commandName), "#!/bin/sh\nexit 0\n")
	}
	writeExecutable(t, filepath.Join(binDir, "launchctl"), "#!/bin/sh\nexit 0\n")

	output, err := runJarvisInstall(t, []string{"PATH=" + binDir + ":" + os.Getenv("PATH")}, "install-server")
	if err == nil {
		t.Fatalf("install-server unexpectedly replaced a loaded service: %s", output)
	}
	if !strings.Contains(output, "already loaded") {
		t.Fatalf("install-server error does not explain the ownership stop:\n%s", output)
	}
}

func runJarvisInstall(t *testing.T, extraEnv []string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", ".agents", "skills", "install-jarvis", "scripts", "jarvis-install"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = append(command.Environ(), extraEnv...)
	output, err := command.CombinedOutput()
	return string(output), err
}
