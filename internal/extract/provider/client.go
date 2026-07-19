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

	"jarvis/internal/extract"
)

const maxResponseBody = 4 << 20

var ErrModelRefusal = errors.New("model refused todo extraction")

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

func (c *Client) Extract(ctx context.Context, prompt extract.Prompt) (*extract.ExtractionResult, error) {
	if strings.TrimSpace(prompt.System) == "" || strings.TrimSpace(prompt.User) == "" {
		return nil, fmt.Errorf("model extraction system and user prompts must be non-empty")
	}
	requestBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": prompt.System},
			{"role": "user", "content": prompt.User},
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name": "todo_extraction", "strict": true, "schema": TodoExtractionJSONSchema(),
			},
		},
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode model extraction request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("create model extraction request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "jarvis/0.1")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send model extraction request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("read model extraction response: %w", err)
	}
	if len(payload) > maxResponseBody {
		return nil, fmt.Errorf("model extraction response exceeds %d bytes", maxResponseBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("model extraction status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var response struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("decode model extraction envelope: %w", err)
	}
	if len(response.Choices) != 1 {
		return nil, fmt.Errorf("model extraction choices=%d, want 1", len(response.Choices))
	}
	choice := response.Choices[0]
	if strings.TrimSpace(choice.Message.Refusal) != "" {
		return nil, fmt.Errorf("%w: %s", ErrModelRefusal, choice.Message.Refusal)
	}
	if choice.FinishReason != "stop" {
		return nil, fmt.Errorf("model extraction finish_reason=%q, want stop", choice.FinishReason)
	}
	if strings.TrimSpace(choice.Message.Content) == "" {
		return nil, fmt.Errorf("model extraction content is empty")
	}
	result, err := extract.DecodeExtractionResult([]byte(choice.Message.Content))
	if err != nil {
		return nil, fmt.Errorf("validate model extraction result: %w", err)
	}
	return result, nil
}
