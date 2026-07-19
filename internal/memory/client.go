package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBody = 1 << 20

// AddInput is one conversation window sent to the mem0 sidecar.
type AddInput struct {
	Transcript string         `json:"messages"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Infer      bool           `json:"infer"`
}

// SearchInput is the stable subset of mem0 search used by downstream modules.
type SearchInput struct {
	Query     string         `json:"query"`
	Filters   map[string]any `json:"filters,omitempty"`
	TopK      int            `json:"top_k"`
	Threshold float64        `json:"threshold"`
	Rerank    bool           `json:"rerank"`
}

// SearchResponse preserves the sidecar result contract for M3 consumers.
type SearchResponse struct {
	Results []map[string]any `json:"results"`
}

// Client is a fail-fast HTTP client for the local Python mem0 sidecar.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("memory client timeout must be positive")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse memory base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("memory base URL must be absolute http(s): %q", baseURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("memory base URL must not contain query or fragment: %q", baseURL)
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
	}, nil
}

func (c *Client) Add(ctx context.Context, input AddInput) error {
	if strings.TrimSpace(input.Transcript) == "" {
		return fmt.Errorf("memory transcript is empty")
	}
	var response struct {
		Results json.RawMessage `json:"results"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/memories", input, &response); err != nil {
		return fmt.Errorf("add memories: %w", err)
	}
	if len(response.Results) == 0 {
		return fmt.Errorf("add memories: response missing results")
	}
	return nil
}

func (c *Client) Search(ctx context.Context, input SearchInput) (*SearchResponse, error) {
	if strings.TrimSpace(input.Query) == "" {
		return nil, fmt.Errorf("memory search query is empty")
	}
	if input.TopK <= 0 {
		return nil, fmt.Errorf("memory search top_k must be positive")
	}
	if input.Threshold < 0 || input.Threshold > 1 {
		return nil, fmt.Errorf("memory search threshold must be between 0 and 1")
	}
	var response SearchResponse
	if err := c.doJSON(ctx, http.MethodPost, "/memories/search", input, &response); err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	return &response, nil
}

func (c *Client) Health(ctx context.Context) error {
	var response struct {
		Status string `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/health", nil, &response); err != nil {
		return fmt.Errorf("memory health: %w", err)
	}
	if response.Status != "ok" {
		return fmt.Errorf("memory health: unexpected status %q", response.Status)
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(payload) > maxResponseBody {
		return fmt.Errorf("response exceeds %d bytes", maxResponseBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
