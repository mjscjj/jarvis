package systemcontrol

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type commandStarter interface {
	Start(string, ...string) error
}

type detachedStarter struct{}

func (detachedStarter) Start(bin string, args ...string) error {
	command := exec.Command(bin, args...)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

type Service struct {
	scriptPath string
	serverPID  int
	starter    commandStarter
	mu         sync.Mutex
	requested  bool
}

func NewService(scriptPath string, serverPID int) (*Service, error) {
	return newService(scriptPath, serverPID, detachedStarter{})
}

func newService(scriptPath string, serverPID int, starter commandStarter) (*Service, error) {
	scriptPath = strings.TrimSpace(scriptPath)
	if scriptPath == "" {
		return nil, fmt.Errorf("shutdown script path is empty")
	}
	info, err := os.Stat(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("load shutdown script: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("shutdown script is not a regular file: %s", scriptPath)
	}
	if serverPID <= 0 {
		return nil, fmt.Errorf("server pid must be positive")
	}
	if starter == nil {
		return nil, fmt.Errorf("shutdown command starter is nil")
	}
	return &Service{scriptPath: scriptPath, serverPID: serverPID, starter: starter}, nil
}

func (s *Service) Shutdown(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.requested {
		return nil
	}
	if err := s.starter.Start(
		"/bin/zsh",
		s.scriptPath,
		"--delay-seconds", "1",
		"--server-pid", strconv.Itoa(s.serverPID),
	); err != nil {
		return fmt.Errorf("start Jarvis shutdown: %w", err)
	}
	s.requested = true
	return nil
}
