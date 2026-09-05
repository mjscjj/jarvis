package execute

import (
	"encoding/json"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/contextpack"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReplayHistoricalPromptSizes(t *testing.T) {
	path := os.Getenv("JARVIS_CONTEXT_REPLAY_DB")
	if path == "" {
		t.Skip("set disposable replay database path")
	}
	db, err := gorm.Open(sqlite.Open("file:"+path+"?mode=ro"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID     uint64
		Prompt string
	}
	if err := db.Table("execution_run").Select("id,prompt").Where("prompt LIKE ?", "%BEGIN_TASK_CONTEXT%execution_context%").Order("id DESC").Limit(100).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var before, after, oldContext, newContext, reductions []float64
	for _, row := range rows {
		prefix, tail, ok := strings.Cut(row.Prompt, "BEGIN_TASK_CONTEXT\n")
		if !ok {
			t.Fatal("missing context")
		}
		block, suffix, ok := strings.Cut(tail, "\nEND_TASK_CONTEXT")
		if !ok {
			t.Fatal("missing context end")
		}
		var old struct {
			Task struct {
				ID        uint64          `json:"id"`
				Title     string          `json:"title_hint"`
				Target    string          `json:"target_hint"`
				Status    string          `json:"current_status"`
				Summary   *string         `json:"current_summary"`
				ProjectID *uint64         `json:"project_id"`
				Source    json.RawMessage `json:"source_payload"`
			} `json:"task"`
			Capture     json.RawMessage   `json:"execution_context"`
			RepoPath    string            `json:"repo_path"`
			Supplements datatypes.JSON    `json:"execution_supplements"`
			Prior       []json.RawMessage `json:"previous_runs"`
		}
		if err := json.Unmarshal([]byte(block), &old); err != nil {
			t.Fatal(err)
		}
		brief := old.Task.Title
		var source struct {
			Payload string `json:"payload"`
		}
		if json.Unmarshal(old.Task.Source, &source) == nil && source.Payload != "" {
			brief = source.Payload
		}
		packet, err := contextpack.Freeze(old.Task.Source, old.Capture, brief, nil)
		if err != nil {
			t.Fatalf("run %d: %v", row.ID, err)
		}
		task := &domain.Task{ID: old.Task.ID, Title: old.Task.Title, Target: old.Task.Target, ActionType: "replay", Status: old.Task.Status, Summary: old.Task.Summary, ProjectID: old.Task.ProjectID, SourcePayload: datatypes.JSON(packet), ExecutionSupplements: old.Supplements}
		_, projected, err := buildTaskContext(task, old.RepoPath, &runHistory{Count: int64(len(old.Prior))})
		if err != nil {
			t.Fatal(err)
		}
		a := float64(utf8.RuneCountInString(row.Prompt))
		b := float64(utf8.RuneCountInString(prefix + "BEGIN_TASK_CONTEXT\n" + string(projected) + "\nEND_TASK_CONTEXT" + suffix))
		before = append(before, a)
		after = append(after, b)
		reductions = append(reductions, 100*(1-b/a))
		oldContext = append(oldContext, float64(utf8.RuneCountInString(block)))
		newContext = append(newContext, float64(utf8.RuneCount(projected)))
	}
	if len(before) == 0 {
		t.Fatal("no old prompts to compare")
	}
	median := func(v []float64) float64 { sort.Float64s(v); return v[len(v)/2] }
	t.Logf("samples=%d Unicode chars: whole median %.0f -> %.0f; context median %.0f -> %.0f; median paired whole reduction %.1f%% (static prompt unchanged)", len(before), median(before), median(after), median(oldContext), median(newContext), median(reductions))
}
