package appservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const RuntimeConnectionPrefix = "JARVIS_RUNTIME_CONNECTION "

type Supervisor struct {
	options Options
	layout  Layout
	client  *http.Client
}

type process struct {
	name string
	cmd  *exec.Cmd
	done chan error
}

func New(options Options) (*Supervisor, error) {
	resolved, err := ResolveOptions(options)
	if err != nil {
		return nil, err
	}
	return &Supervisor{
		options: resolved,
		layout:  NewLayout(resolved),
		client:  &http.Client{Timeout: time.Second},
	}, nil
}

func (s *Supervisor) Layout() Layout {
	return s.layout
}

func (s *Supervisor) Run(ctx context.Context, output io.Writer) error {
	if ctx == nil {
		return fmt.Errorf("app service context is nil")
	}
	if output == nil {
		return fmt.Errorf("app service output is nil")
	}
	if err := Prepare(s.layout); err != nil {
		return err
	}
	release, err := acquireProcessLock(s.layout.LockPath)
	if err != nil {
		return err
	}
	defer release()

	qdrant, err := s.startProcess(
		"qdrant",
		s.layout.QdrantBinary,
		[]string{"--config-path", s.layout.QdrantConfig, "--disable-telemetry"},
		s.layout.QdrantWorking,
		s.layout.QdrantStdoutLog,
		s.layout.QdrantStderrLog,
	)
	if err != nil {
		return err
	}
	processes := []*process{qdrant}
	defer func() {
		stopProcesses(processes)
	}()
	if err := s.waitForHTTP(ctx, "qdrant", s.options.QdrantHealthURL, qdrant); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	server, err := s.startProcess(
		"jarvis-server",
		s.layout.ServerBinary,
		[]string{"-config", s.layout.ConfigPath, "-addr", s.options.Address},
		s.layout.RuntimeRoot,
		s.layout.ServerStdoutLog,
		s.layout.ServerStderrLog,
	)
	if err != nil {
		return err
	}
	processes = append(processes, server)
	if err := s.waitForHTTP(ctx, "jarvis-server", s.layout.Connection(s.options.Address).HTTPURL+"/healthz", server); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	connection := s.layout.Connection(s.options.Address)
	raw, err := json.Marshal(connection)
	if err != nil {
		return fmt.Errorf("encode runtime connection: %w", err)
	}
	if _, err := fmt.Fprintf(output, "%s%s\n", RuntimeConnectionPrefix, raw); err != nil {
		return fmt.Errorf("announce runtime connection: %w", err)
	}

	_ = os.Remove(s.layout.RestartRequestPath)
	parentGone := monitorParent(ctx, s.options.SupervisorPID)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var ccConnect *process
	var ccConnectDone <-chan error
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-parentGone:
			return nil
		case err := <-server.done:
			return processExitError(server.name, err)
		case err := <-qdrant.done:
			return processExitError(qdrant.name, err)
		case err := <-ccConnectDone:
			return processExitError(ccConnect.name, err)
		case <-ticker.C:
			if ccConnect == nil {
				if _, err := os.Stat(s.layout.CCConnectConfig); err == nil {
					ccConnect, err = s.startProcess(
						"cc-connect",
						s.layout.CCConnectBinary,
						[]string{"--config", s.layout.CCConnectConfig},
						s.layout.RuntimeRoot,
						s.layout.CCConnectStdoutLog,
						s.layout.CCConnectStderrLog,
					)
					if err != nil {
						return err
					}
					processes = append(processes, ccConnect)
					ccConnectDone = ccConnect.done
				}
			}
			if _, err := os.Stat(s.layout.RestartRequestPath); err == nil {
				_ = os.Remove(s.layout.RestartRequestPath)
				// Runtime configuration includes CC Connect credentials. Stop the old
				// connection before loading the updated config on the next tick.
				if ccConnect != nil {
					stopProcess(ccConnect, 15*time.Second)
					ccConnect = nil
					ccConnectDone = nil
					processes = processes[:2]
				}
				stopProcess(server, 15*time.Second)
				server, err = s.startProcess(
					"jarvis-server",
					s.layout.ServerBinary,
					[]string{"-config", s.layout.ConfigPath, "-addr", s.options.Address},
					s.layout.RuntimeRoot,
					s.layout.ServerStdoutLog,
					s.layout.ServerStderrLog,
				)
				if err != nil {
					return err
				}
				processes[1] = server
				if err := s.waitForHTTP(ctx, "jarvis-server", connection.HTTPURL+"/healthz", server); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
			}
		}
	}
}

func (s *Supervisor) startProcess(name, binary string, args []string, workingDirectory, stdoutPath, stderrPath string) (*process, error) {
	stdout, err := openLog(stdoutPath)
	if err != nil {
		return nil, err
	}
	stderr, err := openLog(stderrPath)
	if err != nil {
		stdout.Close()
		return nil, err
	}
	command := exec.Command(binary, args...)
	command.Dir = workingDirectory
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = nil
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	home, _ := os.UserHomeDir()
	command.Env = prependPath(
		os.Environ(),
		filepath.Join(s.layout.ResourceRoot, "bin"),
		filepath.Join(home, ".local", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
	)
	command.Env = append(command.Env,
		"JARVIS_DESKTOP=1",
		"JARVIS_RESOURCE_ROOT="+s.layout.ResourceRoot,
		"JARVIS_DESKTOP_STATE_ROOT="+s.layout.StateRoot,
		"JARVIS_RUNTIME_ROOT="+s.layout.RuntimeRoot,
		"JARVIS_CONFIG_PATH="+s.layout.ConfigPath,
	)
	if err := command.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	running := &process{name: name, cmd: command, done: make(chan error, 1)}
	go func() {
		waitErr := command.Wait()
		stdout.Close()
		stderr.Close()
		running.done <- waitErr
		close(running.done)
	}()
	return running, nil
}

func (s *Supervisor) waitForHTTP(ctx context.Context, name, url string, child *process) error {
	deadline := time.NewTimer(s.options.StartupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-child.done:
			return processExitError(name, err)
		case <-deadline.C:
			return fmt.Errorf("%s did not become ready within %s; inspect %s", name, s.options.StartupTimeout, s.stderrPath(name))
		case <-ticker.C:
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			response, err := s.client.Do(request)
			if err != nil {
				continue
			}
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
	}
}

func (s *Supervisor) stderrPath(name string) string {
	if name == "qdrant" {
		return s.layout.QdrantStderrLog
	}
	return s.layout.ServerStderrLog
}

func openLog(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open app service log %q: %w", path, err)
	}
	return file, nil
}

func stopProcesses(processes []*process) {
	for index := len(processes) - 1; index >= 0; index-- {
		stopProcess(processes[index], 4*time.Second)
	}
}

func stopProcess(child *process, timeout time.Duration) {
	if child == nil || child.cmd == nil || child.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-child.cmd.Process.Pid, syscall.SIGTERM)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-child.done:
		return
	case <-timer.C:
		_ = syscall.Kill(-child.cmd.Process.Pid, syscall.SIGKILL)
		<-child.done
	}
}

func processExitError(name string, err error) error {
	if err == nil {
		return fmt.Errorf("%s exited unexpectedly", name)
	}
	return fmt.Errorf("%s exited unexpectedly: %w", name, err)
}

func monitorParent(ctx context.Context, pid int) <-chan struct{} {
	done := make(chan struct{})
	if pid <= 0 {
		return done
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := syscall.Kill(pid, 0)
				if errors.Is(err, syscall.ESRCH) {
					return
				}
			}
		}
	}()
	return done
}

func acquireProcessLock(path string) (func(), error) {
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, writeErr := fmt.Fprintf(file, "%d\n", os.Getpid()); writeErr != nil {
				file.Close()
				os.Remove(path)
				return nil, fmt.Errorf("write app service lock: %w", writeErr)
			}
			if closeErr := file.Close(); closeErr != nil {
				os.Remove(path)
				return nil, fmt.Errorf("close app service lock: %w", closeErr)
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create app service lock: %w", err)
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read existing app service lock: %w", readErr)
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
		if parseErr == nil && pid > 0 && !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return nil, fmt.Errorf("Jarvis app service is already running with pid %d", pid)
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale app service lock: %w", removeErr)
		}
	}
	return nil, fmt.Errorf("could not acquire app service lock")
}
