package execute

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ExecutionSupplement is one execution-time clarification/instruction from a
// human or the proactive Agent. It is isolated from Todo.context_snapshot.
type ExecutionSupplement struct {
	Note    string `json:"note"`
	At      string `json:"at"` // RFC3339 UTC
	Channel string `json:"channel,omitempty"`
}

func decodeExecutionSupplements(raw []byte) ([]ExecutionSupplement, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []ExecutionSupplement
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode execution_supplements: %w", err)
	}
	return items, nil
}

func encodeExecutionSupplements(items []ExecutionSupplement) (json.RawMessage, error) {
	if len(items) == 0 {
		return json.RawMessage("[]"), nil
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, fmt.Errorf("encode execution_supplements: %w", err)
	}
	return json.RawMessage(encoded), nil
}

func appendExecutionSupplement(raw []byte, note, channel string, at time.Time) (json.RawMessage, error) {
	items, err := decodeExecutionSupplements(raw)
	if err != nil {
		return nil, err
	}
	items = append(items, ExecutionSupplement{
		Note: note, At: at.UTC().Format(time.RFC3339), Channel: channel,
	})
	return encodeExecutionSupplements(items)
}

func formatExecutionSupplementDirective(items []ExecutionSupplement) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n【执行阶段补充的信息/指示（来源见每条标签；委托人补充优先于主动巡视维护，可以修正或替换上游线索）】")
	for i, item := range items {
		note := strings.TrimSpace(item.Note)
		if note == "" {
			continue
		}
		source := "委托人"
		if item.Channel == "proactive_agent" {
			source = "主动巡视"
		}
		b.WriteString(fmt.Sprintf("\n%d. 【%s】%s", i+1, source, note))
		if at := strings.TrimSpace(item.At); at != "" {
			b.WriteString(fmt.Sprintf("（%s）", at))
		}
	}
	return b.String()
}
