// Package embedding implements the Volcengine Ark multimodal embedding
// transport used by Todo semantic deduplication.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"jarvis/internal/agentusage"
	"jarvis/internal/ark"
)

const maxResponseBody = 16 << 20

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	http       *http.Client
}

func NewClient(timeout time.Duration) (*Client, error) {
	return newClient(ark.BaseURL, ark.APIKey, ark.EmbeddingModel, ark.EmbeddingDimensions, timeout)
}

func newClient(baseURL, apiKey, model string, dimensions int, timeout time.Duration) (*Client, error) {
	for name, value := range map[string]string{"base_url": baseURL, "api_key": apiKey, "model": model} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("embedding %s must be non-empty", name)
		}
	}
	if dimensions <= 0 {
		return nil, fmt.Errorf("embedding dimensions must be positive")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("embedding timeout must be positive")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse embedding base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("embedding base URL must be absolute http(s): %q", baseURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("embedding base URL must not contain query or fragment")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model,
		dimensions: dimensions, http: &http.Client{Timeout: timeout},
	}, nil
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("embedding input must be non-blank")
	}
	requestBody := struct {
		Model string `json:"model"`
		Input []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"input"`
		EncodingFormat string `json:"encoding_format"`
		Dimensions     int    `json:"dimensions"`
	}{Model: c.model, EncodingFormat: "float", Dimensions: c.dimensions}
	requestBody.Input = append(requestBody.Input, struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{Type: "text", Text: text})
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings/multimodal", bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "jarvis/0.1")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send embedding request: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if len(payload) > maxResponseBody {
		return nil, fmt.Errorf("embedding response exceeds %d bytes", maxResponseBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var response struct {
		Data struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage *struct {
			PromptTokens *int64 `json:"prompt_tokens"`
			TotalTokens  *int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(response.Data.Embedding) != c.dimensions {
		return nil, fmt.Errorf("embedding dimensions=%d, want %d", len(response.Data.Embedding), c.dimensions)
	}
	if response.Usage != nil {
		var inputTokens int64
		switch {
		case response.Usage.PromptTokens != nil:
			inputTokens = *response.Usage.PromptTokens
			if response.Usage.TotalTokens != nil && *response.Usage.TotalTokens != inputTokens {
				return nil, fmt.Errorf("embedding total_tokens=%d differs from prompt_tokens=%d", *response.Usage.TotalTokens, inputTokens)
			}
		case response.Usage.TotalTokens != nil:
			inputTokens = *response.Usage.TotalTokens
		}
		if err := agentusage.Record(ctx, agentusage.Usage{InputTokens: inputTokens, Reported: true}); err != nil {
			return nil, fmt.Errorf("record embedding usage: %w", err)
		}
	}
	vector := make([]float32, c.dimensions)
	for i, value := range response.Data.Embedding {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("embedding value[%d] is not finite", i)
		}
		converted := float32(value)
		if math.IsInf(float64(converted), 0) {
			return nil, fmt.Errorf("embedding value[%d] overflows float32", i)
		}
		vector[i] = converted
	}
	return vector, nil
}
