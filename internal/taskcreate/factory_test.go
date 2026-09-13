package taskcreate

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNormalizeInputDefaultsTodoSourceID(t *testing.T) {
	todoID := uint64(42)
	input, err := normalizeInput(Input{
		TodoID: &todoID, Title: "执行任务", ActionType: "investigate", Target: "目标",
		Background:    json.RawMessage(`{"snapshot_version":"v1"}`),
		SourcePayload: json.RawMessage(`{"instruction":"查清问题"}`),
		SourceType:    SourceTodo,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if input.SourceID == nil || *input.SourceID != todoID {
		t.Fatalf("source_id = %v, want %d", input.SourceID, todoID)
	}
}

// TestNormalizeInputKeepsSourcePayloadVerbatim pins that source semantics ride
// into the Task untouched, regardless of source-specific shape.
func TestNormalizeInputKeepsSourcePayloadVerbatim(t *testing.T) {
	todoID := uint64(42)
	clue := `{"desired_outcome":"产出会议结论与我的待办","semantics":"当前妙记无 view 权限"}`
	input, err := normalizeInput(Input{
		TodoID: &todoID, Title: "会后处理", ActionType: "manual_followup", Target: "公会基建Agent 日会",
		Background:    json.RawMessage(`{"snapshot_version":"v1"}`),
		SourcePayload: json.RawMessage(clue),
		SourceType:    SourceTodo,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if string(input.SourcePayload) != clue {
		t.Fatalf("source_payload = %s, want %s", input.SourcePayload, clue)
	}
}

func TestNormalizeInputRejectsNullSourcePayload(t *testing.T) {
	_, err := normalizeInput(Input{
		Title: "任务", ActionType: "agent_task", Target: "输出结论",
		Background:    json.RawMessage(`{}`),
		SourcePayload: json.RawMessage(`null`),
		SourceType:    SourceManual,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted a null source_payload")
	}
}

func TestNormalizeInputRequiresScheduledOccurrence(t *testing.T) {
	sourceID := uint64(5)
	_, err := normalizeInput(Input{
		Title: "定时任务", ActionType: "agent_task", Target: "会议",
		Background:    json.RawMessage(`{"meeting_number":"123"}`),
		SourcePayload: json.RawMessage(`{"instruction":"加入会议"}`),
		SourceType:    SourceScheduledTask, SourceID: &sourceID,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted scheduled source without occurrence_key")
	}
}

func TestNormalizeInputAcceptsEmptyBackgroundObject(t *testing.T) {
	input, err := normalizeInput(Input{
		Title: "无额外背景任务", ActionType: "agent_task", Target: "输出结论",
		Background:    json.RawMessage(`{}`),
		SourcePayload: json.RawMessage(`{"instruction":"输出结论"}`),
		SourceType:    SourceManual,
	})
	if err != nil {
		t.Fatalf("normalizeInput() error = %v", err)
	}
	if string(input.Background) != `{}` {
		t.Fatalf("background = %s, want {}", input.Background)
	}
}

func TestNormalizeInputRejectsMissingSourcePayload(t *testing.T) {
	_, err := normalizeInput(Input{
		Title: "缺少来源语义", ActionType: "agent_task", Target: "输出结论",
		Background: json.RawMessage(`{}`), SourceType: SourceManual,
	})
	if err == nil {
		t.Fatal("normalizeInput() accepted missing source_payload")
	}
}

func TestNormalizeInputAcceptsOpenSourcePayloadJSON(t *testing.T) {
	for name, payload := range map[string]string{
		"string":  `"直接调查并给出结论"`,
		"array":   `["调查","验证","汇报"]`,
		"number":  `3`,
		"boolean": `true`,
		"object":  `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			input, err := normalizeInput(Input{
				Title: "开放来源语义", ActionType: "agent_task", Target: "输出结论",
				Background: json.RawMessage(`{}`), SourcePayload: json.RawMessage(payload),
				SourceType: SourceManual,
			})
			if err != nil {
				t.Fatalf("normalizeInput() error = %v", err)
			}
			if string(input.SourcePayload) != payload {
				t.Fatalf("source_payload = %s, want %s", input.SourcePayload, payload)
			}
		})
	}
}

func TestNormalizeInputRejectsNullSourcePayloadJSON(t *testing.T) {
	for name, payload := range map[string]string{
		"null":  `null`,
		"blank": ``,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := normalizeInput(Input{
				Title: "空来源语义", ActionType: "agent_task", Target: "输出结论",
				Background: json.RawMessage(`{}`), SourcePayload: json.RawMessage(payload),
				SourceType: SourceManual,
			})
			if err == nil {
				t.Fatalf("normalizeInput() accepted source_payload %s", payload)
			}
		})
	}
}

func TestFactoryPreservesProducerSceneWithoutWorldLookup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "producer.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Task{}, &domain.TaskEvent{}); err != nil {
		t.Fatal(err)
	}
	factory, err := NewFactory(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{SourceManual, SourceProactive, SourceScheduledTask} {
		id := uint64(9)
		occurrence := kind
		task, err := factory.Create(t.Context(), Input{Title: "item", ActionType: "investigate", Target: "display", SourceType: kind, SourceID: &id, OccurrenceKey: &occurrence, Background: json.RawMessage(`{"note":"original scene"}`), SourcePayload: json.RawMessage(`{"instruction":"user request","delivery_required":true,"reply_target":"chat"}`)})
		if err != nil {
			t.Fatal(err)
		}
		var packet map[string]json.RawMessage
		if err := json.Unmarshal(task.SourcePayload, &packet); err != nil {
			t.Fatal(err)
		}
		if string(packet["format_version"]) != "2" || !strings.Contains(string(packet["capture"]), "original scene") || strings.Contains(string(packet["capture"]), "principal") {
			t.Fatalf("packet %s", task.SourcePayload)
		}
		if !strings.Contains(string(packet["source"]), "reply_target") {
			t.Fatal("lost delivery target")
		}
	}
}
