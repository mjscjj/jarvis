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

// Notifier projects M5 state onto one Feishu reply. The first call sends a
// deterministic idempotent reply; every later call edits that exact bot
// message through Feishu's message update API.
type Notifier struct {
	lark larkRunner
}

func NewNotifier(lark larkRunner) (*Notifier, error) {
	if lark == nil {
		return nil, fmt.Errorf("Task feedback lark client is nil")
	}
	return &Notifier{lark: lark}, nil
}

func (n *Notifier) ReplyProcessing(ctx context.Context, taskID uint64, sourceMessageID string) (*execute.TaskFeedbackDelivery, error) {
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if taskID == 0 || sourceMessageID == "" {
		return nil, fmt.Errorf("Task feedback task_id/source_message_id is invalid")
	}
	var response any
	if err := n.lark.Run(ctx, &response,
		"im", "+messages-reply", "--message-id", sourceMessageID,
		"--text", "正在处理中", "--idempotency-key", fmt.Sprintf("jarvis-task-%d-progress", taskID),
		"--as", "bot",
	); err != nil {
		return nil, fmt.Errorf("reply Task processing task_id=%d source_message_id=%s: %w", taskID, sourceMessageID, err)
	}
	messageIDs := distinctMessageIDs(response)
	if len(messageIDs) != 1 {
		return nil, fmt.Errorf("reply Task processing task_id=%d returned %d message_ids, want exactly one", taskID, len(messageIDs))
	}
	return &execute.TaskFeedbackDelivery{MessageID: messageIDs[0]}, nil
}

func (n *Notifier) Update(ctx context.Context, messageID, status, summary string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return fmt.Errorf("Task feedback update message_id is empty")
	}
	text, err := render(status, summary)
	if err != nil {
		return err
	}
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("encode Task feedback text: %w", err)
	}
	body, err := json.Marshal(map[string]string{"msg_type": "text", "content": string(content)})
	if err != nil {
		return fmt.Errorf("encode Task feedback update: %w", err)
	}
	var response any
	if err := n.lark.Run(ctx, &response,
		"api", "PUT", "/open-apis/im/v1/messages/"+messageID,
		"--data", string(body), "--as", "bot",
	); err != nil {
		return fmt.Errorf("update Task feedback message_id=%s status=%s: %w", messageID, status, err)
	}
	return nil
}

func render(status, summary string) (string, error) {
	status = strings.TrimSpace(status)
	summary = strings.TrimSpace(summary)
	switch status {
	case "executing":
		return "正在处理中", nil
	case "awaiting_approval":
		if summary == "" {
			return "等待你的确认后继续处理。", nil
		}
		return summary + "\n\n等待你的确认后继续处理。", nil
	case "needs_human":
		if summary == "" {
			return "需要你补充信息后继续处理。", nil
		}
		return summary, nil
	case "waiting":
		if summary == "" {
			return "正在等待外部条件，满足后会继续处理。", nil
		}
		return summary, nil
	case "done", "observing":
		if summary == "" {
			return "处理完成。", nil
		}
		return summary, nil
	case "failed":
		if summary == "" {
			return "处理失败，请到任务详情查看原因。", nil
		}
		return "处理失败：" + summary, nil
	default:
		return "", fmt.Errorf("unsupported Task feedback status %q", status)
	}
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
