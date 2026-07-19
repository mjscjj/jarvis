// Package embedding implements the OpenAI-compatible embedding transport used
// by mem0 and Todo semantic deduplication.
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
)

const maxResponseBody = 16 << 20

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	http       *http.Client
}

func NewClient(baseURL, apiKey, model string, dimensions int, timeout time.Duration) (*Client, error) {
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
		Model          string `json:"model"`
		Input          string `json:"input"`
		EncodingFormat string `json:"encoding_format"`
	}{Model: c.model, Input: text, EncodingFormat: "float"}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(encoded))
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
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(response.Data) != 1 || response.Data[0].Index != 0 {
		return nil, fmt.Errorf("embedding response data must contain exactly index 0, got %d items", len(response.Data))
	}
	if len(response.Data[0].Embedding) != c.dimensions {
		return nil, fmt.Errorf("embedding dimensions=%d, want %d", len(response.Data[0].Embedding), c.dimensions)
	}
	vector := make([]float32, c.dimensions)
	for i, value := range response.Data[0].Embedding {
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
