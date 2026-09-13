package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"jarvis/internal/agentenv"
	"jarvis/internal/config"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "jarvis-config error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("subcommand is required")
	}
	switch args[0] {
	case "configure-principal":
		return runConfigurePrincipal(args[1:], stdout)
	case "show-principal":
		return runShowPrincipal(args[1:], stdout)
	case "show-connection":
		return runShowConnection(args[1:], stdout)
	case "initialization-status":
		return runInitializationStatus(args[1:], stdout)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// runShowConnection is an installation-time projection of effective config.
// It deliberately emits no identity or credentials and does not probe a server.
func runShowConnection(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("show-connection", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "conf/config.yaml", "base config path")
	address := flags.String("addr", "", "listen address override")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if value := strings.TrimSpace(*address); value != "" {
		cfg.Server.Addr = value
	}
	apiBase, err := agentenv.LocalAPIBase(cfg.Server.Addr)
	if err != nil {
		return err
	}
	if _, err := time.LoadLocation(cfg.Capture.Timezone); err != nil {
		return fmt.Errorf("invalid Agent timezone %q: %w", cfg.Capture.Timezone, err)
	}
	return json.NewEncoder(stdout).Encode(struct {
		APIBase  string `json:"api_base"`
		Timezone string `json:"timezone"`
	}{APIBase: apiBase, Timezone: cfg.Capture.Timezone})
}

func runInitializationStatus(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("initialization-status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "conf/config.yaml", "base config path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	status, err := config.InspectInitialization(*configPath)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(status)
}

func runConfigurePrincipal(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("configure-principal", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "conf/config.yaml", "base config path")
	agentName := flags.String("agent-name", "", "user-selected assistant display name")
	openID := flags.String("open-id", "", "principal open_id for this lark app")
	gitAuthor := flags.String("git-author", "", "git log --author pattern for the principal")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	result, err := config.ConfigurePrincipal(*configPath, *agentName, *openID, *gitAuthor)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

func runShowPrincipal(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("show-principal", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "conf/config.yaml", "base config path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	result := struct {
		AgentDisplayName            string `json:"agent_display_name"`
		PrincipalOpenID             string `json:"principal_open_id"`
		GitAuthor                   string `json:"git_author"`
		CardApprovalEnabled         bool   `json:"card_approval_enabled"`
		CardApprovalPrincipalOpenID string `json:"card_approval_principal_open_id"`
		RelaySecret                 string `json:"relay_secret"`
		RelaySecretSHA256           string `json:"relay_secret_sha256"`
	}{
		AgentDisplayName:            cfg.Identity.DisplayName,
		PrincipalOpenID:             cfg.Extract.PrincipalOpenID,
		GitAuthor:                   cfg.DailyDigest.GitAuthor,
		CardApprovalEnabled:         cfg.CardApproval.Enabled,
		CardApprovalPrincipalOpenID: cfg.CardApproval.PrincipalOpenID,
		RelaySecret:                 cfg.CardApproval.RelaySecret,
		RelaySecretSHA256:           config.RelaySecretSHA256(cfg.CardApproval.RelaySecret),
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}
