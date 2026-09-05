package systemcontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type recordingStarter struct {
	bin   string
	args  []string
	calls int
	err   error
}

func (s *recordingStarter) Start(bin string, args ...string) error {
	s.bin = bin
	s.args = append([]string(nil), args...)
	s.calls++
	return s.err
}

func TestShutdownStartsFixedScriptOnce(t *testing.T) {
	script := filepath.Join(t.TempDir(), "stop-jarvis.sh")
	if err := os.WriteFile(script, []byte("#!/bin/zsh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	starter := &recordingStarter{}
	service, err := newService(script, 42, starter)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if starter.calls != 1 {
		t.Fatalf("starter calls = %d, want 1", starter.calls)
	}
	want := []string{"--delay-seconds", "1", "--server-pid", "42"}
	if starter.bin != script || !reflect.DeepEqual(starter.args, want) {
		t.Fatalf("Start() = %q %#v, want %q %#v", starter.bin, starter.args, script, want)
	}
}

func TestShutdownCanRetryAfterStartFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "stop-jarvis.sh")
	if err := os.WriteFile(script, []byte("#!/bin/zsh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	starter := &recordingStarter{err: errors.New("start failed")}
	service, err := newService(script, 42, starter)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Shutdown(context.Background()); err == nil {
		t.Fatal("Shutdown() error = nil")
	}
	starter.err = nil
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if starter.calls != 2 {
		t.Fatalf("starter calls = %d, want 2", starter.calls)
	}
}

func TestNewServiceRejectsMissingScript(t *testing.T) {
	if _, err := NewService(filepath.Join(t.TempDir(), "missing.sh"), 42); err == nil {
		t.Fatal("NewService() error = nil")
	}
}
