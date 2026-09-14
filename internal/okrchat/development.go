package okrchat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/chat"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/okrworkspace/moduleconfig"
	"jarvis/internal/store"
	"jarvis/internal/textstore"
	"jarvis/internal/toolcatalog"
)

// DevelopmentRuntime executes inside the existing full-stack container. Its
// entire filesystem and API belong to that instance, except shared OKR data.
// Docker's control socket and production main DB are never mounted there.
type DevelopmentRuntime struct {
	container, docker, root string
	cfg                     moduleconfig.ChatConfig
}

func openDevelopment(ctx context.Context, cfg moduleconfig.ChatConfig, root, name string, prompts textstore.Reader) (*chat.Service, func(), error) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return nil, nil, err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(checkCtx, docker, "inspect", "--format", "{{.State.Running}}", cfg.DevelopmentContainer).Output()
	if err != nil || strings.TrimSpace(string(output)) != "true" {
		return nil, nil, fmt.Errorf("development container %q is not running", cfg.DevelopmentContainer)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	db, err := store.OpenSQLite(ctx, config.SQLiteConfig{Path: filepath.Join(root, "chat.db")})
	if err != nil {
		return nil, nil, err
	}
	closeDB := func() { _ = store.Close(db) }
	if err := db.AutoMigrate(domain.ChatModels()...); err != nil {
		closeDB()
		return nil, nil, err
	}
	runtime := &DevelopmentRuntime{container: cfg.DevelopmentContainer, docker: docker, root: root, cfg: cfg}
	service, err := chat.NewService(chat.Options{AgentName: name, Bin: "codex", Model: cfg.Model, Sandbox: "danger-full-access", ReasoningEffort: cfg.ReasoningEffort, Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second, DB: db, FilesRoot: filepath.Join(root, "files"), Prompts: prompts, Runtime: runtime, PromptKey: textstore.SystemPromptOKRChatKey, ToolBlock: toolcatalog.EmilyDevelopmentBlock()})
	if err != nil {
		closeDB()
		return nil, nil, err
	}
	return service, closeDB, nil
}

func (r *DevelopmentRuntime) Prepare(req chat.Request) (chat.Request, error) {
	return (&Runtime{root: r.root}).Prepare(req)
}
func (r *DevelopmentRuntime) DeleteSession(id string) error {
	return (&Runtime{root: r.root}).DeleteSession(id)
}
func (r *DevelopmentRuntime) Stream(ctx context.Context, req chat.Request, prompt string, emit func(chat.Event) error) error {
	if !sessionID.MatchString(req.SessionID) {
		return fmt.Errorf("invalid session ID")
	}
	native := filepath.Join(r.root, "sessions", req.SessionID, "native")
	if err := os.MkdirAll(native, 0700); err != nil {
		return err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	runID := hex.EncodeToString(nonce[:])
	home := "/chat-state/sessions/" + req.SessionID + "/native"
	args := []string{"exec", "-i", "--workdir", "/opt/jarvis", "--env", "HOME=" + home, "--env", "CODEX_HOME=" + home + "/codex", r.container, "emily-dev-agent", runID, "exec"}
	if req.ThreadID != "" {
		args = append(args, "resume", req.ThreadID)
	}
	args = append(args, "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "--model", r.cfg.Model, "-c", "model_reasoning_effort="+strconv.Quote(r.cfg.ReasoningEffort))
	for _, path := range req.ImagePaths {
		args = append(args, "--image", path)
	}
	args = append(args, "-")
	// docker exec cancellation only terminates its client. Stop the process group
	// created by the image's launcher so cancelled tools do not keep writing.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanup, r.docker, "exec", r.container, "bash", "-c", `f="/dev-state/agent-runs/$1.pid"; if [[ -f "$f" ]]; then read -r pid < "$f"; [[ "$pid" =~ ^[0-9]+$ ]] && kill -TERM -- "-$pid" 2>/dev/null; rm -f "$f"; fi`, "cleanup", runID).Run()
	}()
	return chat.StreamCommand(ctx, r.docker, args, prompt, time.Duration(r.cfg.TimeoutSeconds)*time.Second, emit)
}
