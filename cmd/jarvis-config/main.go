package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"jarvis/internal/config"
	"jarvis/internal/okrworkspace/moduleconfig"
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
	case "okr-chat-image":
		flags := flag.NewFlagSet("okr-chat-image", flag.ContinueOnError)
		path := flags.String("config", "conf/config.yaml", "base config path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected positional arguments")
		}
		modulePath := filepath.Join(filepath.Dir(*path), "okr-module.yaml")
		if _, err := os.Stat(modulePath); os.IsNotExist(err) {
			return nil
		}
		cfg, err := moduleconfig.LoadCore(modulePath)
		if err != nil {
			return err
		}
		if err := cfg.Chat.Validate(); err != nil {
			return err
		}
		if cfg.Chat.Enabled {
			_, err = fmt.Fprintln(stdout, cfg.Chat.Image)
		}
		return err
	case "api-base":
		return runInstance(args[1:], stdout, true)
	case "instance":
		return runInstance(args[1:], stdout, false)
	case "configure-principal":
		return runConfigurePrincipal(args[1:], stdout)
	case "show-principal":
		return runShowPrincipal(args[1:], stdout)
	case "initialization-status":
		return runInitializationStatus(args[1:], stdout)
	case "migrate-retired-config":
		flags := flag.NewFlagSet("migrate-retired-config", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		path := flags.String("config", "conf/config.yaml", "base config path")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
		}
		return config.MigrateRetiredConfig(*path)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// Use the same validated base + runtime overlay as jarvis-server. Wildcard
// listen addresses must become loopback destinations for local Agent tools.
func runInstance(args []string, stdout io.Writer, urlOnly bool) error {
	flags := flag.NewFlagSet("api-base", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "conf/config.yaml", "base config path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	instance, err := config.InspectInstance(*configPath)
	if err != nil {
		return err
	}
	if urlOnly {
		_, err = fmt.Fprintln(stdout, instance.APIBase)
		return err
	}
	return json.NewEncoder(stdout).Encode(instance)
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
