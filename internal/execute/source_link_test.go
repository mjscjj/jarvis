package execute

import (
	"testing"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
)

func TestMaterializedTaskAndQuestionShareFrozenTriggerLink(t *testing.T) {
	const link = "https://applink.feishu.cn/client/chat/open?openChatId=oc_group&position=42"
	db := newMaterializerTestDB(t)
	insertMaterializerTodo(t, db, 7, 3)
	payload := frozenTestContent(`{"source_message_ids":["om_context","om_trigger"],"trigger_message_id":"om_trigger"}`, `{"messages":[{"message_id":"om_context","source_url":"https://example.com/other"},{"message_id":"om_trigger","source_url":"`+link+`"}]}`)
	if err := db.Model(&domain.Todo{}).Where("id = ?", 7).Update("content", datatypes.JSON(payload)).Error; err != nil {
		t.Fatal(err)
	}
	materializer, err := NewMaterializer(db)
	if err != nil {
		t.Fatal(err)
	}
	result, err := materializer.MaterializeTodo(t.Context(), 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	var task domain.Task
	if err := db.First(&task, result.TaskID).Error; err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, "frozen source", task.SourcePayload, payload)
	view := taskView(t.Context(), &task)
	if view.SourceURL != link {
		t.Fatalf("task source URL = %q", view.SourceURL)
	}
	if err := projectTaskListItem(&view); err != nil {
		t.Fatal(err)
	}
	if view.SourceURL != link {
		t.Fatalf("list projection lost URL: %q", view.SourceURL)
	}
	task.ExecutionResult = datatypes.JSON(`{"source_run_id":21,"summary":"等待回复","question":{"title":"继续吗","body":"","fields":[{"type":"button","name":"go","label":"继续"}]}}`)
	notice, err := questionSnapshot(&task)
	if err != nil {
		t.Fatal(err)
	}
	if notice.SourceURL != view.SourceURL {
		t.Fatalf("card and task links differ: %q != %q", notice.SourceURL, view.SourceURL)
	}
	// A later Todo revision cannot redirect a previously materialized task.
	if err := db.Model(&domain.Todo{}).Where("id = ?", 7).Update("content", datatypes.JSON(`{}`)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&task, result.TaskID).Error; err != nil {
		t.Fatal(err)
	}
	if got := taskView(t.Context(), &task).SourceURL; got != link {
		t.Fatalf("task origin changed with Todo: %q", got)
	}
}
