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

// Notifier projects M5 state onto the source Feishu message. Execution start
// adds a best-effort OnIt reaction; the first result creates one deterministic
// reply and later results edit that exact bot message. It only ever delivers the
// model's own user_message, verbatim: what the conversation should hear — down to
// whom the text mentions — is decided upstream by the model, and an empty text
// means it chose to stay silent, so callers must skip delivery entirely rather
// than ask the transport for a status placeholder.
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

func (n *Notifier) ReplyResult(ctx context.Context, taskID uint64, target execute.TaskFeedbackTarget, userMessage string) (*execute.TaskFeedbackDelivery, error) {
	sourceMessageID := strings.TrimSpace(target.SourceMessageID)
	if taskID == 0 || sourceMessageID == "" {
		return nil, fmt.Errorf("Task feedback task_id/source_message_id is invalid")
	}
	text := strings.TrimSpace(userMessage)
	if text == "" {
		return nil, fmt.Errorf("Task feedback reply text is empty task_id=%d", taskID)
	}
	args := []string{
		"im", "+messages-reply", "--message-id", sourceMessageID,
		"--text", text, "--idempotency-key", fmt.Sprintf("jarvis-task-%d-result", taskID),
		"--as", "bot",
	}
	if target.ReplyInThread {
		args = append(args, "--reply-in-thread")
	}
	var response any
	if err := n.lark.Run(ctx, &response, args...); err != nil {
		return nil, fmt.Errorf("reply Task result task_id=%d source_message_id=%s: %w", taskID, sourceMessageID, err)
	}
	messageIDs := distinctMessageIDs(response)
	if len(messageIDs) != 1 {
		return nil, fmt.Errorf("reply Task result task_id=%d returned %d message_ids, want exactly one", taskID, len(messageIDs))
	}
	return &execute.TaskFeedbackDelivery{MessageID: messageIDs[0], Preview: text}, nil
}

func (n *Notifier) UpdateResult(ctx context.Context, messageID, userMessage string) (string, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", fmt.Errorf("Task feedback update message_id is empty")
	}
	text := strings.TrimSpace(userMessage)
	if text == "" {
		return "", fmt.Errorf("Task feedback update text is empty message_id=%s", messageID)
	}
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return "", fmt.Errorf("encode Task feedback text: %w", err)
	}
	body, err := json.Marshal(map[string]string{"msg_type": "text", "content": string(content)})
	if err != nil {
		return "", fmt.Errorf("encode Task feedback update: %w", err)
	}
	var response any
	if err := n.lark.Run(ctx, &response,
		"api", "PUT", "/open-apis/im/v1/messages/"+messageID,
		"--data", string(body), "--as", "bot",
	); err != nil {
		return "", fmt.Errorf("update Task feedback message_id=%s: %w", messageID, err)
	}
	return text, nil
}

func distinctMessageIDs(value any) []string {
	set := make(map[string]struct{})
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, nested := range typed {
				if key == "message_id" {
					if id, ok := nested.(string); ok && strings.HasPrefix(strings.TrimSpace(id), "om_") {
						set[strings.TrimSpace(id)] = struct{}{}
					}
				}
				visit(nested)
			}
		case []any:
			for _, nested := range typed {
				visit(nested)
			}
		case json.RawMessage:
			var nested any
			if json.Unmarshal(typed, &nested) == nil {
				visit(nested)
			}
		}
	}
	visit(value)
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	return result
}
