package taskfeedback

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"jarvis/internal/execute"
)

type larkRunner interface {
	Run(context.Context, any, ...string) error
}

const processingEmoji = "OnIt"

// Notifier projects only M5's best-effort execution-start acknowledgement onto
// the source Feishu message. Ordinary business messages are explicit M5 tool
// actions and never pass through this package.
type Notifier struct {
	lark larkRunner
}

func NewNotifier(lark larkRunner) (*Notifier, error) {
	if lark == nil {
		return nil, fmt.Errorf("Task feedback lark client is nil")
	}
	return &Notifier{lark: lark}, nil
}

func (n *Notifier) AddProcessingReaction(ctx context.Context, target execute.TaskFeedbackTarget) (*execute.TaskFeedbackReaction, error) {
	sourceMessageID := strings.TrimSpace(target.SourceMessageID)
	if sourceMessageID == "" {
		return nil, fmt.Errorf("Task feedback source_message_id is invalid")
	}
	params, err := json.Marshal(map[string]string{"message_id": sourceMessageID})
	if err != nil {
		return nil, fmt.Errorf("encode Task processing reaction params: %w", err)
	}
	data, err := json.Marshal(map[string]any{
		"reaction_type": map[string]string{"emoji_type": processingEmoji},
	})
	if err != nil {
		return nil, fmt.Errorf("encode Task processing reaction data: %w", err)
	}
	var response struct {
		Data struct {
			ReactionID string `json:"reaction_id"`
		} `json:"data"`
	}
	if err := n.lark.Run(ctx, &response,
		"im", "reactions", "create",
		"--params", string(params), "--data", string(data), "--as", "bot",
	); err != nil {
		return nil, fmt.Errorf("add Task processing reaction source_message_id=%s: %w", sourceMessageID, err)
	}
	reactionID := strings.TrimSpace(response.Data.ReactionID)
	if reactionID == "" {
		return nil, fmt.Errorf("add Task processing reaction source_message_id=%s returned no reaction_id", sourceMessageID)
	}
	return &execute.TaskFeedbackReaction{ReactionID: reactionID}, nil
}
