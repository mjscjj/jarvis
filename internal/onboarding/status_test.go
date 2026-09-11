package onboarding

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/domain"
)

func TestStatusChecksIndependentLoginsConcurrently(t *testing.T) {
	root := t.TempDir()
	base, err := os.ReadFile("../../conf/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, base, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.PrincipalProfile{}, &domain.Task{}); err != nil {
		t.Fatal(err)
	}
	agentStarted := make(chan struct{})
	larkStarted := make(chan struct{})
	service := &Service{options: Options{ConfigPath: path, StateRoot: root, DB: db}}
	service.runner = commandFunc(func(ctx context.Context, bin string, args []string, input string) ([]byte, error) {
		if bin == "bash" && len(args) == 3 && args[1] == "check" && filepath.Base(args[0]) == "jarvis-lark-auth" {
			return []byte(`{"ok":true}`), nil
		}
		if len(args) > 0 && args[0] == "event" {
			return (onboardingRunnerStub{}).Run(ctx, bin, args, input)
		}
		var other <-chan struct{}
		switch strings.Join(args, " ") {
		case "login status":
			close(agentStarted)
			other = larkStarted
		case "auth status --json --verify":
			close(larkStarted)
			other = agentStarted
		default:
			t.Errorf("unexpected startup command: %v", args)
			return nil, errors.New("unexpected command")
		}
		select {
		case <-other:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if args[0] == "login" {
			// A failed check must remain visible even when the other succeeds.
			return []byte("Not logged in"), errors.New("exit 1")
		}
		return (onboardingRunnerStub{}).Run(ctx, bin, args, input)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	status, err := service.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("login checks waited for each other")
	}
	if !status.Lark.User.Verified || status.Agent.Authenticated || status.Agent.Error == "" || status.AppReady {
		t.Fatalf("independent check results were lost: %+v", status)
	}
}

func TestAgentStatusUsesOnlyLoginCheck(t *testing.T) {
	for _, test := range []struct {
		name          string
		output        string
		err           error
		available     bool
		authenticated bool
	}{
		{name: "logged in", output: "Logged in using Trae", available: true, authenticated: true},
		{name: "login required", output: "Not logged in", err: errors.New("exit 1"), available: true},
		{name: "binary missing", err: exec.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			service := &Service{runner: commandFunc(func(ctx context.Context, _ string, args []string, _ string) ([]byte, error) {
				calls++
				if strings.Join(args, " ") != "login status" {
					t.Fatalf("unexpected login command: %v", args)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("status command has no deadline")
				}
				return []byte(test.output), test.err
			})}
			status := service.agentStatus(t.Context())
			if calls != 1 || status.Available != test.available || status.Authenticated != test.authenticated || (status.Error != "") != (test.err != nil) {
				t.Fatalf("calls=%d status=%+v", calls, status)
			}
		})
	}
}

func TestLarkStatusHonorsCancellation(t *testing.T) {
	service := &Service{runner: commandFunc(func(ctx context.Context, _ string, _ []string, _ string) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("status command has no deadline")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	status := service.larkStatus(ctx)
	if status.Error == "" || status.User.Verified || status.Bot.Verified {
		t.Fatalf("cancelled check reported ready: %+v", status)
	}
}
