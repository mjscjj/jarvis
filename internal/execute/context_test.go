package execute

import (
	"encoding/json"
	"errors"
	"jarvis/internal/contextpack"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
	"strings"
	"testing"
)

func frozenTestContent(source, capture string) datatypes.JSON {
	raw, err := contextpack.Freeze([]byte(source), []byte(capture), "fixture", nil)
	if err != nil {
		panic(err)
	}
	return datatypes.JSON(raw)
}

func TestTaskListPreviewAndFullDetail(t *testing.T) {
	db := newMaterializerTestDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"failed", "needs_human", "waiting"} {
		task := domain.Task{Title: "预览", Target: "对象", ActionType: "investigate", Status: status, SourceType: "manual",
			SourcePayload:   frozenTestContent(`{"request":"一级原文","opaque":{"huge":1e400,"id":9007199254740993}}`, `{"project":{"name":"项目名","summary":"完整项目正文"},"group":{"name":"群名"},"assigner":{"name":"发起人"}}`),
			ExecutionResult: datatypes.JSON(`{"stage":"interrupted","summary":"摘要","error":"错误","question":{"title":"问题标题","body":"完整问题正文"},"waiting":{"reason":"等待原因","wake_at":"2026-09-05T10:00:00Z"},"effects":[{"kind":"effect","body":"完整副作用"}]}`),
		}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.ListTasks(t.Context(), TaskFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 3 {
		t.Fatalf("items: %d", len(list.Items))
	}
	for _, item := range list.Items {
		preview, _ := json.Marshal(item)
		for _, want := range []string{"项目名", "群名", "发起人", "摘要", "错误", "问题标题", "等待原因", "interrupted"} {
			if !strings.Contains(string(preview), want) {
				t.Fatalf("missing %s: %s", want, preview)
			}
		}
		for _, hidden := range []string{"一级原文", "完整项目正文", "完整问题正文", "完整副作用"} {
			if strings.Contains(string(preview), hidden) {
				t.Fatalf("list leaked %s", hidden)
			}
		}
		detail, err := store.GetTask(t.Context(), item.ID)
		if err != nil {
			t.Fatal(err)
		}
		full, _ := json.Marshal(detail)
		for _, want := range []string{"一级原文", "完整项目正文", "完整问题正文", "完整副作用"} {
			if !strings.Contains(string(full), want) {
				t.Fatalf("detail lost %s", want)
			}
		}
	}
}

func TestRunPagesOmitBodiesAndMissingRun(t *testing.T) {
	db := newMaterializerTestDB(t)
	if err := db.Migrator().CreateTable(&domain.ExecutionRun{}); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	errorDetail := "完整错误正文"
	for i := 0; i < 25; i++ {
		run := domain.ExecutionRun{TaskID: 1, Status: "failed", ErrorDetail: &errorDetail, Prompt: "完整提示词", Output: datatypes.JSON(`{"body":"完整结果"}`), Effects: datatypes.JSON(`[{"body":"完整副作用"}]`)}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	for page, count := range map[int]int{1: 20, 2: 5, 3: 0} {
		result, err := store.ListRuns(t.Context(), 1, RunFilter{Page: page, PageSize: 20})
		if err != nil {
			t.Fatal(err)
		}
		if result.Total != 25 || result.Page != page || result.PageSize != 20 || len(result.Items) != count {
			t.Fatalf("page: %#v", result)
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), "完整") {
			t.Fatalf("run index leaked bodies: %s", raw)
		}
	}
	if _, err := store.GetRun(t.Context(), 999); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing run: %v", err)
	}
	if _, err := store.GetRun(t.Context(), 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid run: %v", err)
	}
}

func TestExecutionProgressiveEvidenceAndHistory(t *testing.T) {
	original := strings.Repeat("完整原文", 1000)
	capture, _ := json.Marshal(map[string]any{
		"messages":        []map[string]any{{"message_id": "link", "content": "https://code.example/mr/456"}, {"message_id": "request", "content": original}},
		"project":         map[string]any{"summary": strings.Repeat("大段背景", 2000)},
		"unknown_capture": map[string]any{"keep": true},
	})
	task := &domain.Task{ID: 1, Title: "review", ActionType: "review", SourcePayload: frozenTestContent(`{"source_message_ids":["link","request"],"assessment":"待调查"}`, string(capture))}
	_, raw, err := buildTaskContext(task, "", &runHistory{Count: 4, LatestID: 9, LatestStatus: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(raw)
	for _, want := range []string{original, "https://code.example/mr/456", `"history":`, `"count":4`} {
		if !strings.Contains(prompt, want) {
			t.Fatal("missing expected primary evidence or history")
		}
	}
	for _, unwanted := range []string{"大段背景", "unknown_capture\":{", "current_world", "previous_runs"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("eager context contains %s", unwanted)
		}
	}
	body, err := contextpack.Read(task.SourcePayload, "unknown_capture", "")
	if err != nil || !strings.Contains(string(body), `"keep":true`) {
		t.Fatalf("lost original: %s %v", body, err)
	}
}

func TestRelatedWorkQueryAndFailedRunDrillDown(t *testing.T) {
	db := newMaterializerTestDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"todo", "manual", "scheduled_task", "proactive"} {
		task := domain.Task{Title: "核查 MR 456", Target: "MR456", ActionType: "review", Status: "done", SourceType: source, SourcePayload: frozenTestContent(`{"source_message_ids":["mr456"],"request":"review"}`, `{"messages":[{"message_id":"mr456","content":"MR链接"}]}`)}
		if err := db.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListTasks(t.Context(), TaskFilter{Query: "456", SourceMessageID: "mr456", Page: 2, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || len(page.Items) != 2 {
		t.Fatalf("query/pagination lost sources: %#v", page)
	}
	for _, item := range page.Items {
		if len(item.SourcePayload) > 0 || len(item.SourceMessageIDs) == 0 {
			t.Fatalf("list did not return compact refs: %#v", item)
		}
	}
	if err := db.Migrator().CreateTable(&domain.ExecutionRun{}); err != nil {
		t.Fatal(err)
	}
	run := domain.ExecutionRun{TaskID: 1, Status: "failed", Output: datatypes.JSON(`{"step":"later step failed"}`), Effects: datatypes.JSON(`[{"kind":"new_effect","id":123}]`), Prompt: "long prompt"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	executor := &AgentExecutor{store: store}
	history, err := executor.loadRunHistory(t.Context(), 1, 0)
	if err != nil || history.Count != 1 || history.LatestStatus != "failed" {
		t.Fatalf("history: %#v %v", history, err)
	}
	index, err := store.ListRuns(t.Context(), 1, RunFilter{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Items[0].Output) > 0 && string(index.Items[0].Output) != "null" {
		t.Fatal("run list leaked output")
	}
	detail, err := store.GetRun(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(detail.Effects), "new_effect") || !strings.Contains(string(detail.Output), "later step failed") {
		t.Fatal("lost failure side effects")
	}
}

func TestTaskSearchFindsKeywordOnlyInFrozenMessage(t *testing.T) {
	db := newMaterializerTestDB(t)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{Title: "审查", Target: "对象", ActionType: "review", Status: "done", SourceType: "todo",
		SourcePayload: frozenTestContent(`{"source_message_ids":["om_source"],"payload":"请处理"}`, `{"messages":[{"message_id":"om_source","content":"https://example.test/only-in-original-456"}]}`)}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	list, err := store.ListTasks(t.Context(), TaskFilter{Query: "only-in-original-456", SourceMessageID: "om_source", Page: 1, PageSize: 20})
	if err != nil || list.Total != 1 {
		t.Fatalf("source body search: %#v %v", list, err)
	}
	wrong, err := store.ListTasks(t.Context(), TaskFilter{SourceMessageID: "message:om_source", Page: 1, PageSize: 20})
	if err != nil || wrong.Total != 0 {
		t.Fatalf("invented key matched: %#v %v", wrong, err)
	}
}
