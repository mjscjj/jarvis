// Package provider implements the OpenAI-compatible structured-output
// transport used by M3. It has no fallback to JSON mode or regex parsing.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"jarvis/internal/agentusage"
	"jarvis/internal/extract"
)

const maxResponseBody = 4 << 20

var ErrModelRefusal = errors.New("model refused structured output")

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func NewClient(baseURL, apiKey, model string, timeout time.Duration) (*Client, error) {
	for name, value := range map[string]string{"base_url": baseURL, "api_key": apiKey, "model": model} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("model %s must be non-empty", name)
		}
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("model timeout must be positive")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse model base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("model base URL must be absolute http(s): %q", baseURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("model base URL must not contain query or fragment")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model,
		http: &http.Client{Timeout: timeout},
	}, nil
}

func (c *Client) SameAction(ctx context.Context, incoming extract.Candidate, existing extract.SemanticTodo) (bool, error) {
	if err := extract.ValidateCandidate(&incoming); err != nil {
		return false, fmt.Errorf("semantic adjudication incoming candidate: %w", err)
	}
	if existing.ID == 0 || strings.TrimSpace(existing.Title) == "" || strings.TrimSpace(existing.ActionType) == "" {
		return false, fmt.Errorf("semantic adjudication existing Todo is invalid")
	}
	if incoming.ActionType != existing.ActionType {
		return false, fmt.Errorf("semantic adjudication action types differ: %s vs %s", incoming.ActionType, existing.ActionType)
	}
	pair, err := json.Marshal(map[string]any{"incoming": incoming, "existing": existing})
	if err != nil {
		return false, fmt.Errorf("encode semantic adjudication pair: %w", err)
	}
	prompt := extract.Prompt{
		System: "You are a strict Todo deduplication classifier. Return same_action=true only when both records refer to the same concrete real-world action and deliverable. Similar topics, repositories, people, or action types are not enough. If scope, target, or intended outcome differs, return false.",
		User:   string(pair),
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"same_action": map[string]any{"type": "boolean"},
		},
		"required":             []string{"same_action"},
		"additionalProperties": false,
	}
	payload, err := c.completeStructured(ctx, "semantic adjudication", "todo_same_action", schema, prompt)
	if err != nil {
		return false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var result struct {
		SameAction bool `json:"same_action"`
	}
	if err := decoder.Decode(&result); err != nil {
		return false, fmt.Errorf("decode semantic adjudication result: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return false, fmt.Errorf("decode semantic adjudication result: multiple JSON values")
		}
		return false, fmt.Errorf("decode semantic adjudication trailing JSON: %w", err)
	}
	return result.SameAction, nil
}

// Complete 是一次不带 response_format 的普通 chat completion：给定 system/user
// 提示词，返回 assistant 的纯文本。fail-fast：拒答/非 stop 结束/空内容都直接
// 报错，不静默返回空串。
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	if strings.TrimSpace(system) == "" {
		return "", fmt.Errorf("model complete system prompt is empty")
	}
	if strings.TrimSpace(user) == "" {
		return "", fmt.Errorf("model complete user prompt is empty")
	}
	requestBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	choice, err := c.postChatCompletion(ctx, "text completion", requestBody)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(choice.Message.Refusal) != "" {
		return "", fmt.Errorf("%w: %s", ErrModelRefusal, choice.Message.Refusal)
	}
	if choice.FinishReason != "stop" {
		return "", fmt.Errorf("model text completion finish_reason=%q, want stop", choice.FinishReason)
	}
	content := strings.TrimSpace(choice.Message.Content)
	if content == "" {
		return "", fmt.Errorf("model text completion content is empty")
	}
	return content, nil
}

func (c *Client) completeStructured(ctx context.Context, operation, schemaName string, schema map[string]any, prompt extract.Prompt) ([]byte, error) {
	requestBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": prompt.System},
			{"role": "user", "content": prompt.User},
		},
		"response_format": structuredResponseFormat(schemaName, schema),
	}
	choice, err := c.postChatCompletion(ctx, operation, requestBody)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(choice.Message.Refusal) != "" {
		return nil, fmt.Errorf("%w: %s", ErrModelRefusal, choice.Message.Refusal)
	}
	if choice.FinishReason != "stop" {
		return nil, fmt.Errorf("model %s finish_reason=%q, want stop", operation, choice.FinishReason)
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		return nil, fmt.Errorf("model %s content is empty", operation)
	}
	return []byte(choice.Message.Content), nil
}

// chatToolCall mirrors one OpenAI-compatible tool call in an assistant message.
type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatChoice is the single choice we require from every completion response.
type chatChoice struct {
	FinishReason string `json:"finish_reason"`
	Message      struct {
		Content   string         `json:"content"`
		Refusal   string         `json:"refusal"`
		ToolCalls []chatToolCall `json:"tool_calls"`
	} `json:"message"`
}

// structuredResponseFormat builds the strict json_schema response_format block
// shared by the single-shot and tool-loop completions.
func structuredResponseFormat(schemaName string, schema map[string]any) map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name": schemaName, "strict": true, "schema": schema,
		},
	}
}

// postChatCompletion sends one chat completion request and returns the sole
// choice. It owns transport, size limits, status handling and the "exactly one
// choice" contract; callers interpret finish_reason / tool_calls / content.
func (c *Client) postChatCompletion(ctx context.Context, operation string, requestBody map[string]any) (*chatChoice, error) {
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode model %s request: %w", operation, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("create model %s request: %w", operation, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "jarvis/0.1")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send model %s request: %w", operation, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("read model %s response: %w", operation, err)
	}
	if len(payload) > maxResponseBody {
		return nil, fmt.Errorf("model %s response exceeds %d bytes", operation, maxResponseBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("model %s status=%d body=%s", operation, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var response struct {
		Choices []chatChoice    `json:"choices"`
		Usage   json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("decode model %s envelope: %w", operation, err)
	}
	if len(response.Choices) != 1 {
		return nil, fmt.Errorf("model %s choices=%d, want 1", operation, len(response.Choices))
	}
	if len(response.Usage) > 0 && !bytes.Equal(response.Usage, []byte("null")) {
		usage, err := decodeChatUsage(response.Usage)
		if err != nil {
			return nil, fmt.Errorf("decode model %s usage: %w", operation, err)
		}
		if err := agentusage.Record(ctx, usage); err != nil {
			return nil, fmt.Errorf("record model %s usage: %w", operation, err)
		}
	}
	return &response.Choices[0], nil
}

func decodeChatUsage(payload []byte) (agentusage.Usage, error) {
	var raw struct {
		InputTokens           *int64 `json:"input_tokens"`
		OutputTokens          *int64 `json:"output_tokens"`
		CachedInputTokens     *int64 `json:"cached_input_tokens"`
		ReasoningOutputTokens *int64 `json:"reasoning_output_tokens"`
		PromptTokens          *int64 `json:"prompt_tokens"`
		CompletionTokens      *int64 `json:"completion_tokens"`
		PromptTokensDetails   struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		CompletionTokensDetails struct {
			ReasoningTokens *int64 `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return agentusage.Usage{}, err
	}
	usage := agentusage.Usage{Reported: true}
	if raw.InputTokens != nil {
		usage.InputTokens = *raw.InputTokens
	} else if raw.PromptTokens != nil {
		usage.InputTokens = *raw.PromptTokens
	}
	if raw.OutputTokens != nil {
		usage.OutputTokens = *raw.OutputTokens
	} else if raw.CompletionTokens != nil {
		usage.OutputTokens = *raw.CompletionTokens
	}
	if raw.CachedInputTokens != nil {
		usage.CachedInputTokens = *raw.CachedInputTokens
	} else if raw.PromptTokensDetails.CachedTokens != nil {
		usage.CachedInputTokens = *raw.PromptTokensDetails.CachedTokens
	}
	if raw.ReasoningOutputTokens != nil {
		usage.ReasoningOutputTokens = *raw.ReasoningOutputTokens
	} else if raw.CompletionTokensDetails.ReasoningTokens != nil {
		usage.ReasoningOutputTokens = *raw.CompletionTokensDetails.ReasoningTokens
	}
	if err := usage.Validate(); err != nil {
		return agentusage.Usage{}, err
	}
	return usage, nil
}
