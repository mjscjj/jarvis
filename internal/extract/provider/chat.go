package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"jarvis/internal/extract"
)

// ExtractWithTools runs M3 extraction as a function-calling loop: the model may
// call retrieval tools (chat history, memory) before emitting the final strict
// JSON. It fails fast — an unknown tool, a tool error, or a refusal surfaces as
// an error rather than a degraded result. The caller's context is the loop's
// termination boundary; there is no tool-call count cap.
//
// Every round requests the strict extraction schema as response_format, so the
// first round without tool calls returns the final structured result directly.
func (c *Client) ExtractWithTools(ctx context.Context, prompt extract.Prompt, box extract.ToolBox) (*extract.ExtractionResult, error) {
	if strings.TrimSpace(prompt.System) == "" || strings.TrimSpace(prompt.User) == "" {
		return nil, fmt.Errorf("model extraction system and user prompts must be non-empty")
	}
	if box == nil {
		return nil, fmt.Errorf("model extraction tool box is nil")
	}
	messages := []map[string]any{
		{"role": "system", "content": prompt.System},
		{"role": "user", "content": prompt.User},
	}
	specs := box.Specs()
	responseFormat := structuredResponseFormat("todo_extraction", TodoExtractionJSONSchema())

	for {
		requestBody := map[string]any{
			"model":           c.model,
			"messages":        messages,
			"response_format": responseFormat,
		}
		if len(specs) > 0 {
			requestBody["tools"] = specs
			requestBody["tool_choice"] = "auto"
		}
		choice, err := c.postChatCompletion(ctx, "extraction", requestBody)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(choice.Message.Refusal) != "" {
			return nil, fmt.Errorf("%w: %s", ErrModelRefusal, choice.Message.Refusal)
		}

		if len(choice.Message.ToolCalls) > 0 {
			if err := c.appendToolResults(ctx, &messages, box, choice.Message.ToolCalls); err != nil {
				return nil, err
			}
			continue
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
}

// appendToolResults records the assistant tool-call turn, runs each requested
// tool, and appends its result as a role:tool message keyed by tool_call_id.
func (c *Client) appendToolResults(ctx context.Context, messages *[]map[string]any, box extract.ToolBox, calls []chatToolCall) error {
	assistantCalls := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			return fmt.Errorf("model tool call is missing an id")
		}
		if strings.TrimSpace(call.Function.Name) == "" {
			return fmt.Errorf("model tool call %q is missing a function name", call.ID)
		}
		assistantCalls = append(assistantCalls, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Function.Name,
				"arguments": call.Function.Arguments,
			},
		})
	}
	*messages = append(*messages, map[string]any{
		"role":       "assistant",
		"content":    "",
		"tool_calls": assistantCalls,
	})

	for _, call := range calls {
		result, err := box.Invoke(ctx, call.Function.Name, json.RawMessage(call.Function.Arguments))
		if err != nil {
			return err
		}
		*messages = append(*messages, map[string]any{
			"role":         "tool",
			"tool_call_id": call.ID,
			"content":      string(result),
		})
	}
	return nil
}
