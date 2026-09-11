package onboarding

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"jarvis/internal/config"
)

func TestBootstrapOnlyReadsLocalConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	base, err := os.ReadFile("../../conf/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, base, 0o600); err != nil {
		t.Fatal(err)
	}
	service := &Service{options: Options{ConfigPath: path}, runner: commandFunc(func(context.Context, string, []string, string) ([]byte, error) {
		t.Fatal("bootstrap must not start a CLI or validate external credentials")
		return nil, nil
	})}
	status, err := service.Bootstrap()
	if err != nil || status.MachineConfigurationReady {
		t.Fatalf("fresh install: %+v, %v", status, err)
	}
	if _, err := config.ConfigurePrincipal(path, "Jarvis", "ou_test", "test@example.com"); err != nil {
		t.Fatal(err)
	}
	status, err = service.Bootstrap()
	if err != nil || !status.MachineConfigurationReady {
		t.Fatalf("installed: %+v, %v", status, err)
	}
	service.options.Desktop = true
	status, err = service.Bootstrap()
	if err != nil || status.MachineConfigurationReady {
		t.Fatalf("desktop must apply saved configuration before fast entry: %+v, %v", status, err)
	}
	service.runtimeConfigured = true
	status, err = service.Bootstrap()
	if err != nil || !status.MachineConfigurationReady {
		t.Fatalf("configured desktop runtime: %+v, %v", status, err)
	}
	if err := os.WriteFile(path, []byte("invalid: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bootstrap(); err == nil {
		t.Fatal("invalid local configuration must remain visible")
	}
}
