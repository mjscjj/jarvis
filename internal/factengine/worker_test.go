package factengine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeCursorStore struct {
	cursors  map[string]uint64
	advanced map[string][]uint64
}

func (f *fakeCursorStore) Cursor(_ context.Context, source string) (uint64, bool, error) {
	value, ok := f.cursors[source]
	return value, ok, nil
}

func (f *fakeCursorStore) AdvanceCursor(_ context.Context, source string, lastID uint64, _ time.Time) error {
	if f.cursors == nil {
		f.cursors = map[string]uint64{}
	}
	if f.advanced == nil {
		f.advanced = map[string][]uint64{}
	}
	f.cursors[source] = lastID
	f.advanced[source] = append(f.advanced[source], lastID)
	return nil
}

type fakeMaintainer struct {
	result string
	err    error
	calls  int
	system string
	user   string
}

func (f *fakeMaintainer) Maintain(_ context.Context, system, user string) (string, error) {
	f.calls++
	f.system, f.user = system, user
	if f.err != nil {
		return "", f.err
	}
	if f.result == "" {
		return "NOTHING", nil
	}
	return f.result, nil
}

type fakePrompts struct{ content string }

func (f fakePrompts) Content(context.Context, string) (string, error) { return f.content, nil }

func materialSource(name string, maxID uint64, units func(limit int) []SourceUnit) MaterialSource {
	return MaterialSource{
		Name:  name,
		MaxID: func(context.Context) (uint64, error) { return maxID, nil },
		Units: func(_ context.Context, _ uint64, limit int, _ WindowOptions) ([]SourceUnit, uint64, error) {
			selected := units(limit)
			if len(selected) == 0 {
				return nil, 0, nil
			}
			return selected, selected[len(selected)-1].LastID, nil
		},
	}
}

func testUnit(source, key string, lastID uint64, body string) SourceUnit {
	return SourceUnit{Source: source, Key: key, LastID: lastID, OccurredAt: time.Unix(int64(lastID), 0), Body: body}
}

func newTestWorker(t *testing.T, store *fakeCursorStore, sources []MaterialSource, maintainer worldMaintainer, maxChars int) *Worker {
	t.Helper()
	worker, err := NewWorker(store, sources, maintainer, WorkerOptions{
		BatchLimit: 50, MaxMaterialChars: maxChars,
		Window:  WindowOptions{Gap: 30 * time.Minute, MaxMessages: 40, Location: time.UTC},
		Prompts: fakePrompts{content: "维护世界模型"},
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return worker
}

func TestExtractOnceRunsOneAgentForAllNewMaterial(t *testing.T) {
	store := &fakeCursorStore{cursors: map[string]uint64{SourceMessage: 1, SourceTask: 10}}
	sources := []MaterialSource{
		materialSource(SourceMessage, 2, func(int) []SourceUnit { return []SourceUnit{testUnit(SourceMessage, "m:2", 2, "消息原文")} }),
		materialSource(SourceTask, 11, func(int) []SourceUnit { return []SourceUnit{testUnit(SourceTask, "t:11", 11, "任务结果")} }),
	}
	maintainer := &fakeMaintainer{result: "更新了项目状态并写入一条事实"}
	stats, err := newTestWorker(t, store, sources, maintainer, 100000).ExtractOnce(t.Context())
	if err != nil {
		t.Fatalf("ExtractOnce: %v", err)
	}
	if maintainer.calls != 1 || stats.Calls != 1 || stats.Units != 2 {
		t.Fatalf("maintainer calls=%d stats=%+v", maintainer.calls, stats)
	}
	for _, want := range []string{"WORLD_CHANGES", "MATERIAL_SOURCE: message", "消息原文", "MATERIAL_SOURCE: task", "任务结果"} {
		if !strings.Contains(maintainer.user, want) {
			t.Fatalf("batch prompt missing %q:\n%s", want, maintainer.user)
		}
	}
	if fmt.Sprint(store.advanced[SourceMessage]) != "[2]" || fmt.Sprint(store.advanced[SourceTask]) != "[11]" {
		t.Fatalf("advanced=%v", store.advanced)
	}
}

func TestExtractOnceShrinksRowLimitToCoarseCharacterBudget(t *testing.T) {
	store := &fakeCursorStore{cursors: map[string]uint64{SourceTask: 1}}
	var limits []int
	source := materialSource(SourceTask, 51, func(limit int) []SourceUnit {
		limits = append(limits, limit)
		units := make([]SourceUnit, min(limit, 8))
		for i := range units {
			units[i] = testUnit(SourceTask, fmt.Sprintf("t:%d", i+2), uint64(i+2), strings.Repeat("字", 1000))
		}
		return units
	})
	stats, err := newTestWorker(t, store, []MaterialSource{source}, &fakeMaintainer{}, 3500).ExtractOnce(t.Context())
	if err != nil {
		t.Fatalf("ExtractOnce: %v", err)
	}
	if len(limits) < 2 || limits[len(limits)-1] >= limits[0] {
		t.Fatalf("row limits=%v, want coarse halving", limits)
	}
	if stats.MaterialChars > 3500 {
		t.Fatalf("material chars=%d, want <=3500", stats.MaterialChars)
	}
}

func TestExtractOnceDefersWholeSourceWhenMinimumRowsExceedBudget(t *testing.T) {
	store := &fakeCursorStore{cursors: map[string]uint64{SourceTodo: 1, SourceTask: 10}}
	sources := []MaterialSource{
		materialSource(SourceTodo, 2, func(int) []SourceUnit {
			return []SourceUnit{testUnit(SourceTodo, "todo:2", 2, strings.Repeat("待", 2500))}
		}),
		materialSource(SourceTask, 11, func(int) []SourceUnit {
			return []SourceUnit{testUnit(SourceTask, "task:11", 11, strings.Repeat("任", 2500))}
		}),
	}
	maintainer := &fakeMaintainer{}
	stats, err := newTestWorker(t, store, sources, maintainer, 3200).ExtractOnce(t.Context())
	if err != nil {
		t.Fatalf("ExtractOnce: %v", err)
	}
	if stats.MaterialChars > 3200 || stats.Units != 1 {
		t.Fatalf("stats=%+v, want one whole source within budget", stats)
	}
	if fmt.Sprint(store.advanced[SourceTodo]) != "[2]" || len(store.advanced[SourceTask]) != 0 {
		t.Fatalf("advanced=%v, want deferred task cursor unchanged", store.advanced)
	}
	if strings.Contains(maintainer.user, "task:11") {
		t.Fatalf("deferred source leaked into prompt:\n%s", maintainer.user)
	}
}

func TestExtractOnceKeepsAllMaterialCursorsWhenAgentFails(t *testing.T) {
	store := &fakeCursorStore{cursors: map[string]uint64{SourceMessage: 1, SourceTask: 10}}
	sources := []MaterialSource{
		materialSource(SourceMessage, 2, func(int) []SourceUnit { return []SourceUnit{testUnit(SourceMessage, "m", 2, "m")} }),
		materialSource(SourceTask, 11, func(int) []SourceUnit { return []SourceUnit{testUnit(SourceTask, "t", 11, "t")} }),
	}
	_, err := newTestWorker(t, store, sources, &fakeMaintainer{err: errors.New("model unavailable")}, 100000).ExtractOnce(t.Context())
	if err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("ExtractOnce error=%v", err)
	}
	if len(store.advanced) != 0 {
		t.Fatalf("advanced=%v, want none", store.advanced)
	}
}

func TestExtractOnceSkipsAgentWhenThereIsNoNewMaterial(t *testing.T) {
	store := &fakeCursorStore{cursors: map[string]uint64{SourceMessage: 1}}
	source := materialSource(SourceMessage, 1, func(int) []SourceUnit { return nil })
	maintainer := &fakeMaintainer{}
	stats, err := newTestWorker(t, store, []MaterialSource{source}, maintainer, 100000).ExtractOnce(t.Context())
	if err != nil {
		t.Fatalf("ExtractOnce: %v", err)
	}
	if maintainer.calls != 0 || stats.Calls != 0 {
		t.Fatalf("maintainer calls=%d stats=%+v", maintainer.calls, stats)
	}
}

func TestExtractOnceSeedsPresentOnlyMessageSource(t *testing.T) {
	store := &fakeCursorStore{}
	source := materialSource(SourceMessage, 8421, func(int) []SourceUnit { return nil })
	source.StartAtPresent = true
	stats, err := newTestWorker(t, store, []MaterialSource{source}, &fakeMaintainer{}, 100000).ExtractOnce(t.Context())
	if err != nil {
		t.Fatalf("ExtractOnce: %v", err)
	}
	if !stats.Sources[0].Seeded || stats.Sources[0].LastID != 8421 || fmt.Sprint(store.advanced[SourceMessage]) != "[8421]" {
		t.Fatalf("stats=%+v advanced=%v", stats, store.advanced)
	}
}

func TestBuildWorldBatchPromptEscapesInvalidUTF8WithoutDroppingByte(t *testing.T) {
	unit := testUnit(SourceMessage, "m", 1, "before"+string([]byte{0xff})+"after")
	prompt, err := buildWorldBatchPrompt([]selectedSource{{units: []SourceUnit{unit}}})
	if err != nil {
		t.Fatalf("buildWorldBatchPrompt: %v", err)
	}
	if !strings.Contains(prompt, `before\xFFafter`) || !utf8Valid(prompt) {
		t.Fatalf("prompt=%q", prompt)
	}
}

func utf8Valid(value string) bool {
	return !strings.ContainsRune(value, '\uFFFD') && strings.ToValidUTF8(value, "") == value
}

func TestBuildAgentSystemPromptOwnsDirectFactWrites(t *testing.T) {
	prompt, err := buildAgentSystemPrompt("维护长期事实与当前世界状态")
	if err != nil {
		t.Fatalf("buildAgentSystemPrompt: %v", err)
	}
	for _, want := range []string{"当前阶段：factengine", "通用查询及 CRUD", "`append-fact` 直接写入", "不创建或推进 Task"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestNewWorkerRejectsIncompleteOptions(t *testing.T) {
	store := &fakeCursorStore{}
	source := materialSource(SourceMessage, 0, func(int) []SourceUnit { return nil })
	valid := WorkerOptions{BatchLimit: 1, MaxMaterialChars: 1000, Window: WindowOptions{Gap: time.Minute, MaxMessages: 1, Location: time.UTC}, Prompts: fakePrompts{content: "x"}}
	for _, tt := range []struct {
		name string
		edit func(*WorkerOptions)
		want string
	}{
		{"batch limit", func(o *WorkerOptions) { o.BatchLimit = 0 }, "batch limit"},
		{"material chars", func(o *WorkerOptions) { o.MaxMaterialChars = 0 }, "material chars"},
		{"window gap", func(o *WorkerOptions) { o.Window.Gap = 0 }, "window gap"},
		{"window max", func(o *WorkerOptions) { o.Window.MaxMessages = 0 }, "window max"},
		{"location", func(o *WorkerOptions) { o.Window.Location = nil }, "location"},
		{"prompts", func(o *WorkerOptions) { o.Prompts = nil }, "prompt reader"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := valid
			tt.edit(&opts)
			if _, err := NewWorker(store, []MaterialSource{source}, &fakeMaintainer{}, opts); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewWorker error=%v, want %q", err, tt.want)
			}
		})
	}
}
