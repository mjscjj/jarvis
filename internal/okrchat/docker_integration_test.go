package okrchat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/chat"
	"jarvis/internal/okrworkspace/moduleconfig"
)

type integrationPrompts struct{}

func (integrationPrompts) Content(context.Context, string) (string, error) {
	return "Follow the user's test instruction. Do not access unrelated files or data.", nil
}

// Explicit opt-in: these tests launch real containers and the model test makes
// two small paid/model-account turns. Neither touches the product OKR database.
func TestDockerNetworkAndFilesystem(t *testing.T) {
	if os.Getenv("OKR_DOCKER_TEST") != "1" {
		t.Skip("set OKR_DOCKER_TEST=1")
	}
	repo, _ := filepath.Abs("../..")
	root := t.TempDir()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("OKR_ONLY")) }))
	defer upstream.Close()
	handler, err := NewAccessHandler(upstream.URL, []string{"api.openai.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	socketDir, err := os.MkdirTemp("", "okr-socket-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	socket := filepath.Join(socketDir, "access.sock")
	srv, err := ListenAccess(socket, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAccess(srv)
	auth := filepath.Join(root, "auth.json")
	if err := os.WriteFile(auth, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"native", "work", "files"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	r := &Runtime{root: root, repo: repo, socket: socket, auth: auth, config: moduleconfig.ChatConfig{Image: "jarvis-okr-chat:0.154.0"}}
	args := r.args(chat.Request{}, "jarvis-okr-network-test", filepath.Join(root, "native"), filepath.Join(root, "work"))
	i := 0
	for ; i < len(args); i++ {
		if args[i] == r.config.Image {
			break
		}
	}
	args = append(args[:i], "--entrypoint", "bash", r.config.Image, "-c", `
set -euo pipefail
socat TCP-LISTEN:18080,bind=127.0.0.1,reuseaddr,fork UNIX-CONNECT:/run/okr-access.sock &
for i in $(seq 1 30); do if curl -fsS http://127.0.0.1:18080/api/okr/board >/tmp/board 2>/dev/null; then break; fi; sleep .1; done
test "$(cat /tmp/board)" = OKR_ONLY
test "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18080/api/chat/sessions)" = 403
test "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:18080/api/tasks)" = 403
if curl --noproxy '*' --connect-timeout 1 -fsS http://1.1.1.1 >/dev/null 2>&1; then exit 31; fi
test ! -e /var/run/docker.sock
test ! -e /opt/jarvis/data
test ! -e /native/config.toml
test -r /opt/jarvis/scripts/jarvis-tools
test -d /opt/jarvis/.agents/skills
if touch /opt/jarvis/scripts/okr-isolation-test 2>/dev/null; then exit 32; fi
node -e 'require("http").get("http://127.0.0.1:18080/api/messages",r=>process.exit(r.statusCode===403?0:33)).on("error",()=>process.exit(34))'
printf 'DOCKER_BOUNDARY_OK\n'
`)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("boundary test: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "DOCKER_BOUNDARY_OK") {
		t.Fatalf("missing result: %s", out)
	}
}

func TestDockerModelAndResume(t *testing.T) {
	auth := os.Getenv("OKR_CHAT_AUTH_FILE")
	if auth == "" {
		t.Skip("set OKR_CHAT_AUTH_FILE for two real model turns")
	}
	repo, _ := filepath.Abs("../..")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"marker":"OKR_TOOL_PROOF"}}`))
	}))
	defer upstream.Close()
	cfg := moduleconfig.ChatConfig{Enabled: true, Image: "jarvis-okr-chat:0.154.0", Model: "gpt-5.6-sol", ReasoningEffort: "medium", TimeoutSeconds: 120, AuthFile: auth, ModelHosts: []string{"chatgpt.com:443", "auth.openai.com:443", "api.openai.com:443"}}
	s, closeService, err := Open(t.Context(), cfg, t.TempDir(), repo, upstream.URL, "Jarvis", integrationPrompts{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeService()
	session, err := s.CreateSession(t.Context(), chat.CreateSessionInput{Agent: "codex", Model: cfg.Model, ReasoningEffort: cfg.ReasoningEffort})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"Remember the marker ORANGE-OKR-742. Use curl once to read $JARVIS_API_BASE/api/okr/board and answer with both markers only.", "Without using tools, repeat the two markers from the preceding turn."} {
		var reply strings.Builder
		err := s.StreamSession(t.Context(), session.ID, chat.SendInput{Message: prompt}, func(e chat.Event) error {
			if e.Kind == chat.EventDelta {
				reply.WriteString(e.Text)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("model turn failed: %v", err)
		}
		for _, marker := range []string{"ORANGE-OKR-742", "OKR_TOOL_PROOF"} {
			if !strings.Contains(reply.String(), marker) {
				t.Fatalf("missing %s in %q", marker, reply.String())
			}
		}
	}
	view, err := s.GetSession(t.Context(), session.ID)
	if err != nil || len(view.Messages) != 4 {
		t.Fatalf("history persistence: %+v %v", view, err)
	}
	finished := make(chan error, 1)
	go func() {
		finished <- s.StreamSession(context.Background(), session.ID, chat.SendInput{Message: "Use a shell to sleep 120 seconds before replying."}, func(chat.Event) error { return nil })
	}()
	filter := "label=jarvis.okr-chat.session=" + session.ID
	deadline := time.Now().Add(15 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		out, err := exec.Command("docker", "ps", "-q", "--filter", filter).Output()
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(out)) != "" {
			found = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		t.Fatal("model container did not start")
	}
	if !s.CancelSession(session.ID) {
		t.Fatal("session was not running")
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled turn succeeded")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancel did not terminate container")
	}
	out, err := exec.Command("docker", "ps", "-aq", "--filter", filter).Output()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("cancel left container: %s %v", out, err)
	}
}
