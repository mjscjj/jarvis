package toolcatalog

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/config"
)

func TestToolsInheritOwningInstanceAcrossWorkingDirectories(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("JARVIS_CONFIG", "")
	t.Setenv("JARVIS_API_BASE", "")
	t.Setenv("PATH", os.Getenv("PATH"))
	for _, origin := range []string{"instance-A", "instance-B"} {
		t.Run(origin, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"code":0,"data":{"origin":%q,"content":%q}}`, origin, origin)
			}))
			defer server.Close()
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			addr := strings.TrimPrefix(server.URL, "http://")
			if err := os.WriteFile(configPath, []byte("server:\n  addr: "+addr+"\nchat:\n  addr: 127.0.0.1:18801\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, inherited := range []bool{false, true} {
				// Outside an Agent, the explicit config chooses the instance.
				// Inside an Agent, the server exports its URL and local tool PATH.
				t.Setenv("JARVIS_CONFIG", configPath)
				t.Setenv("JARVIS_API_BASE", "")
				if inherited {
					cfg := config.Config{Server: config.ServerConfig{Addr: addr}}
					if err := cfg.ExportToolEnvironment(configPath, root); err != nil {
						t.Fatal(err)
					}
				}
				for _, args := range [][]string{
					{"jarvis-tools", "get-principal"},
					{"okr-module-tools", "scope"},
					{"biz-okr-tools", "scope"},
					{"okr-agent-tools", "prompt", "--key", "okr_agent_principles"},
					{"jarvis-world-model", "discover"},
				} {
					binary := filepath.Join(root, "scripts", args[0])
					if inherited {
						binary = args[0]
					}
					cmd := exec.CommandContext(t.Context(), binary, args[1:]...)
					cmd.Dir = t.TempDir()
					output, err := cmd.CombinedOutput()
					if err != nil || !strings.Contains(string(output), origin) {
						t.Fatalf("inherited=%v command=%v output=%s error=%v", inherited, args, output, err)
					}
				}
				link := filepath.Join(t.TempDir(), "jarvis-tools")
				if err := os.Symlink(filepath.Join(root, "scripts", "jarvis-tools"), link); err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(t.Context(), link, "get-principal")
				cmd.Dir = t.TempDir()
				output, err := cmd.CombinedOutput()
				if err != nil || !strings.Contains(string(output), origin) {
					t.Fatalf("symlink inherited=%v output=%s error=%v", inherited, output, err)
				}
			}
		})
	}
}
