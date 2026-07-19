package execute

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// gitRepo runs git commands against a single working tree. It is intentionally
// minimal: create a branch, commit, read the diff. Everything is fail-fast — a
// dirty tree or any non-zero git exit aborts the run rather than papering over
// an inconsistent state.
type gitRepo struct {
	path    string
	timeout time.Duration
}

func newGitRepo(path string, timeout time.Duration) *gitRepo {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &gitRepo{path: path, timeout: timeout}
}

func (g *gitRepo) run(ctx context.Context, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = g.path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, limitedText(out, 2048))
	}
	return string(out), nil
}

// ensureClean fails if the working tree has uncommitted changes. Jarvis must
// start from a known-clean state so the committed diff is exactly codex's work.
func (g *gitRepo) ensureClean(ctx context.Context) error {
	out, err := g.run(ctx, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("repo %s is not clean; refusing to run code change:\n%s", g.path, strings.TrimSpace(out))
	}
	return nil
}

// currentBranch returns the branch name we started from, so we can report it.
func (g *gitRepo) currentBranch(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// createBranch checks out a fresh branch from the current HEAD.
func (g *gitRepo) createBranch(ctx context.Context, branch string) error {
	_, err := g.run(ctx, "checkout", "-b", branch)
	return err
}

// hasChanges reports whether the working tree changed after codex ran.
func (g *gitRepo) hasChanges(ctx context.Context) (bool, error) {
	out, err := g.run(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// commitAll stages everything and commits. It never pushes.
func (g *gitRepo) commitAll(ctx context.Context, message string) (string, error) {
	if _, err := g.run(ctx, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, "commit", "-m", message); err != nil {
		return "", err
	}
	out, err := g.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// diffAgainst returns the unified diff between base and HEAD.
func (g *gitRepo) diffAgainst(ctx context.Context, base string) (string, error) {
	return g.run(ctx, "diff", base+"...HEAD")
}
