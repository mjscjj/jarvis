package okrchat

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

type Runtime struct {
	config                           moduleconfig.ChatConfig
	root, repo, socket, auth, docker string
	access                           *http.Server
}

// Open keeps the conversation DB outside all mounts. Only this product's
// attachments, per-session native state, scripts, and Skills enter Docker.
func Open(ctx context.Context, cfg moduleconfig.ChatConfig, root, repo, upstream, name string, prompts textstore.Reader) (*chat.Service, func(), error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		return nil, nil, fmt.Errorf("OKR chat requires Docker: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	repo, err = filepath.Abs(repo)
	if err != nil {
		return nil, nil, err
	}
	auth, err := filepath.Abs(cfg.AuthFile)
	if err != nil {
		return nil, nil, err
	}
	if st, err := os.Stat(auth); err != nil || !st.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("OKR chat auth_file must be an existing model login file")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(checkCtx, docker, "image", "inspect", cfg.Image).CombinedOutput(); err != nil {
		return nil, nil, fmt.Errorf("OKR image unavailable: %w: %s", err, output)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, nil, err
	}
	if err := cleanupContainers(checkCtx, docker, root); err != nil {
		return nil, nil, err
	}
	// Linux Unix socket paths are short; use a private temporary directory.
	socketDir, err := os.MkdirTemp("", "jarvis-okr-chat-")
	if err != nil {
		return nil, nil, err
	}
	r := &Runtime{config: cfg, root: root, repo: repo, auth: auth, docker: docker, socket: filepath.Join(socketDir, "access.sock")}
	handler, err := NewAccessHandler(upstream, cfg.ModelHosts)
	if err != nil {
		os.Remove(socketDir)
		return nil, nil, err
	}
	r.access, err = ListenAccess(r.socket, handler)
	if err != nil {
		os.Remove(socketDir)
		return nil, nil, err
	}
	cleanup := func() { closeAccess(r.access); _ = os.Remove(r.socket); _ = os.Remove(socketDir) }
	db, err := store.OpenSQLite(ctx, config.SQLiteConfig{Path: filepath.Join(root, "chat.db")})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeAll := func() { cleanup(); _ = store.Close(db) }
	if err := db.AutoMigrate(domain.ChatModels()...); err != nil {
		closeAll()
		return nil, nil, err
	}
	s, err := chat.NewService(chat.Options{AgentName: name, Bin: "codex", Model: cfg.Model, Sandbox: "danger-full-access", ReasoningEffort: cfg.ReasoningEffort, Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second, DB: db, FilesRoot: filepath.Join(root, "files"), Prompts: prompts, Runtime: r, PromptKey: textstore.SystemPromptOKRChatKey, ToolBlock: toolcatalog.OKRChatBlock()})
	if err != nil {
		closeAll()
		return nil, nil, err
	}
	return s, closeAll, nil
}

var sessionID = regexp.MustCompile(`^cs_[a-zA-Z0-9]+$`)

func (r *Runtime) DeleteSession(id string) error {
	if !sessionID.MatchString(id) {
		return fmt.Errorf("invalid isolated session ID")
	}
	return os.RemoveAll(filepath.Join(r.root, "sessions", id))
}

func instanceID(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:16])
}

// Only this instance's labelled containers are ours to recover after a crash.
func cleanupContainers(ctx context.Context, docker, root string) error {
	output, err := exec.CommandContext(ctx, docker, "ps", "-aq", "--filter", "label=jarvis.okr-chat.instance="+instanceID(root)).Output()
	if err != nil {
		return fmt.Errorf("list interrupted OKR containers: %w", err)
	}
	validID := regexp.MustCompile(`^[a-f0-9]{12,64}$`)
	for _, id := range strings.Fields(string(output)) {
		if !validID.MatchString(id) {
			return fmt.Errorf("invalid Docker container ID")
		}
		if output, err := exec.CommandContext(ctx, docker, "rm", "-f", id).CombinedOutput(); err != nil {
			return fmt.Errorf("remove interrupted OKR container: %w: %s", err, output)
		}
	}
	return nil
}

func (r *Runtime) Prepare(req chat.Request) (chat.Request, error) {
	if !sessionID.MatchString(req.SessionID) {
		return req, fmt.Errorf("invalid isolated session ID")
	}
	mapPaths := func(paths []string) ([]string, error) {
		out := make([]string, len(paths))
		for i, p := range paths {
			rel, err := filepath.Rel(filepath.Join(r.root, "files"), p)
			if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
				return nil, fmt.Errorf("attachment is outside OKR files")
			}
			out[i] = "/attachments/" + filepath.ToSlash(rel)
		}
		return out, nil
	}
	var err error
	req.AttachmentPaths, err = mapPaths(req.AttachmentPaths)
	if err != nil {
		return req, err
	}
	req.ImagePaths, err = mapPaths(req.ImagePaths)
	if err != nil {
		return req, err
	}
	req.VisibleHistory = strings.ReplaceAll(req.VisibleHistory, filepath.Join(r.root, "files")+"/", "/attachments/")
	return req, nil
}

func (r *Runtime) args(req chat.Request, name, native, workspace string) []string {
	args := []string{"run", "--rm", "--init", "--name", name, "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "256", "--memory", "2g", "--cpus", "2", "--user", strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid()), "--tmpfs", "/tmp:rw,nosuid,nodev,size=256m", "--workdir", "/workspace", "-i"}
	args = append(args, "--label", "jarvis.okr-chat.session="+req.SessionID)
	args = append(args, "--label", "jarvis.okr-chat.instance="+instanceID(r.root))
	mount := func(source, target string, ro bool) {
		value := "type=bind,src=" + source + ",dst=" + target
		if ro {
			value += ",readonly"
		}
		args = append(args, "--mount", value)
	}
	mount(r.socket, "/run/okr-access.sock", true)
	mount(r.auth, "/credentials/auth.json", true)
	mount(filepath.Join(r.repo, "scripts"), "/opt/jarvis/scripts", true)
	mount(filepath.Join(r.repo, ".agents", "skills"), "/opt/jarvis/.agents/skills", true)
	mount(filepath.Join(r.root, "files"), "/attachments", true)
	mount(native, "/native", false)
	mount(workspace, "/workspace", false)
	args = append(args, r.config.Image, "exec")
	if req.ThreadID != "" {
		args = append(args, "resume", req.ThreadID)
	}
	args = append(args, "--json", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "--model", r.config.Model, "-c", "model_reasoning_effort="+strconv.Quote(r.config.ReasoningEffort))
	for _, p := range req.ImagePaths {
		args = append(args, "--image", p)
	}
	return append(args, "-")
}

func (r *Runtime) Stream(ctx context.Context, req chat.Request, prompt string, emit func(chat.Event) error) error {
	if !sessionID.MatchString(req.SessionID) {
		return fmt.Errorf("invalid isolated session ID")
	}
	native := filepath.Join(r.root, "sessions", req.SessionID, "native")
	workspace := filepath.Join(r.root, "sessions", req.SessionID, "work")
	for _, dir := range []string{native, workspace, filepath.Join(r.root, "files")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	name := "jarvis-okr-" + hex.EncodeToString(random[:])
	// Killing docker's attached client alone does not stop the container. Force
	// removal is required on success, timeout, parser errors and user cancellation.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanup, r.docker, "rm", "-f", name).Run()
	}()
	return chat.StreamCommand(ctx, r.docker, r.args(req, name, native, workspace), prompt, time.Duration(r.config.TimeoutSeconds)*time.Second, emit)
}
