package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type AgentView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
	Default   bool   `json:"default"`
}

type ModelView struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	Description            string   `json:"description,omitempty"`
	InputModalities        []string `json:"input_modalities,omitempty"`
	ReasoningEfforts       []string `json:"reasoning_efforts,omitempty"`
	DefaultReasoningEffort string   `json:"default_reasoning_effort,omitempty"`
	Default                bool     `json:"default"`
}

func agentBinary(id string) string {
	if id == "trae" {
		return "traex"
	}
	if id == "cursor" {
		return "cursor-agent"
	}
	return "codex"
}

func (s *Service) ListAgents(ctx context.Context) []AgentView {
	result := make([]AgentView, 0, 3)
	for _, item := range []struct{ id, name string }{{"codex", "Codex"}, {"trae", "TRAE"}, {"cursor", "Cursor"}} {
		view := AgentView{ID: item.id, Name: item.name, Default: item.id == s.runner.agent}
		path, err := exec.LookPath(agentBinary(item.id))
		if err != nil {
			view.Error = "未安装"
			result = append(result, view)
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out, err := exec.CommandContext(probeCtx, path, "--version").CombinedOutput()
		cancel()
		if err != nil {
			view.Error = strings.TrimSpace(string(out))
			if view.Error == "" {
				view.Error = err.Error()
			}
		} else {
			view.Available = true
			view.Version = strings.TrimSpace(string(out))
		}
		result = append(result, view)
	}
	return result
}

func (s *Service) ListModels(ctx context.Context, agent string) ([]ModelView, error) {
	agent, err := normalizeAgent(agent)
	if err != nil {
		return nil, err
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	switch agent {
	case "codex":
		return discoverCodexModels(discoveryCtx)
	case "trae":
		return discoverTRAEModels(discoveryCtx)
	case "cursor":
		return discoverCursorModels(discoveryCtx)
	}
	return nil, fmt.Errorf("unknown agent %q", agent)
}

func discoverTRAEModels(ctx context.Context) ([]ModelView, error) {
	out, err := exec.CommandContext(ctx, "traex", "models", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("discover TRAE models: %w", err)
	}
	return parseTRAEModels(out)
}

func parseTRAEModels(out []byte) ([]ModelView, error) {
	var rows []struct {
		Name               string   `json:"name"`
		RealName           string   `json:"real_name"`
		Description        string   `json:"description"`
		SupportedMIMETypes []string `json:"supported_mime_types"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("decode TRAE models: %w", err)
	}
	result := make([]ModelView, 0, len(rows))
	for i, row := range rows {
		name := row.RealName
		if name == "" {
			name = row.Name
		}
		modalities := []string{"text"}
		for _, m := range row.SupportedMIMETypes {
			if strings.HasPrefix(m, "image/") {
				modalities = append(modalities, "image")
				break
			}
		}
		result = append(result, ModelView{ID: row.Name, Name: name, Description: row.Description, InputModalities: modalities, Default: i == 0})
	}
	return result, nil
}

var cursorModelLine = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s+-\s+(.+)$`)

func discoverCursorModels(ctx context.Context) ([]ModelView, error) {
	out, err := exec.CommandContext(ctx, "cursor-agent", "--list-models").Output()
	if err != nil {
		return nil, fmt.Errorf("discover Cursor models: %w", err)
	}
	return parseCursorModels(string(out))
}

func parseCursorModels(output string) ([]ModelView, error) {
	result := []ModelView{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		match := cursorModelLine.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if len(match) != 3 {
			continue
		}
		result = append(result, ModelView{ID: match[1], Name: match[2], InputModalities: []string{"text"}, Default: match[1] == "auto"})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Cursor returned no parseable models")
	}
	return result, nil
}

func discoverCodexModels(ctx context.Context) ([]ModelView, error) {
	command := exec.CommandContext(ctx, "codex", "app-server", "--stdio")
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Codex app-server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}()
	encoder, decoder := json.NewEncoder(stdin), json.NewDecoder(bufio.NewReader(stdout))
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "jarvis-chat", "version": "1"}}}); err != nil {
		return nil, err
	}
	if _, err := readRPCResult(decoder, 1); err != nil {
		return nil, fmt.Errorf("initialize Codex app-server: %w", err)
	}
	if err := encoder.Encode(map[string]any{"method": "initialized"}); err != nil {
		return nil, err
	}
	var result []ModelView
	var cursor any
	requestID := 2
	for {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != nil {
			params["cursor"] = cursor
		}
		if err := encoder.Encode(map[string]any{"id": requestID, "method": "model/list", "params": params}); err != nil {
			return nil, err
		}
		raw, err := readRPCResult(decoder, requestID)
		if err != nil {
			return nil, fmt.Errorf("list Codex models: %w", err)
		}
		var page struct {
			Data []struct {
				ID, Model, DisplayName, Description, DefaultReasoningEffort string
				Hidden, IsDefault                                           bool
				InputModalities                                             []string
				SupportedReasoningEfforts                                   []struct{ ReasoningEffort string }
			} `json:"data"`
			NextCursor any `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode Codex models: %w", err)
		}
		for _, row := range page.Data {
			if row.Hidden {
				continue
			}
			id := row.Model
			if id == "" {
				id = row.ID
			}
			efforts := make([]string, 0, len(row.SupportedReasoningEfforts))
			for _, e := range row.SupportedReasoningEfforts {
				efforts = append(efforts, e.ReasoningEffort)
			}
			result = append(result, ModelView{ID: id, Name: row.DisplayName, Description: row.Description, InputModalities: row.InputModalities, ReasoningEfforts: efforts, DefaultReasoningEffort: row.DefaultReasoningEffort, Default: row.IsDefault})
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
		requestID++
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Codex returned no models")
	}
	return result, nil
}

func readRPCResult(decoder *json.Decoder, id int) (json.RawMessage, error) {
	for {
		var message struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  any             `json:"error"`
		}
		if err := decoder.Decode(&message); err != nil {
			return nil, err
		}
		if message.ID == nil || *message.ID != id {
			continue
		}
		if message.Error != nil {
			return nil, fmt.Errorf("%v", message.Error)
		}
		return message.Result, nil
	}
}
