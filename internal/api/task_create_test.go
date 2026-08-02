package api

import (
	"testing"

	"jarvis/internal/taskcreate"
)

func TestCreateTaskInputDefaultsManualSource(t *testing.T) {
	input, err := createTaskInput(createTaskRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if input.SourceType != taskcreate.SourceManual || input.ActorType != "user" {
		t.Fatalf("input = %+v", input)
	}
}

func TestCreateTaskInputAcceptsProactiveSource(t *testing.T) {
	input, err := createTaskInput(createTaskRequest{SourceType: taskcreate.SourceProactive})
	if err != nil {
		t.Fatal(err)
	}
	if input.SourceType != taskcreate.SourceProactive || input.ActorType != "proactive" || input.EventDetail["channel"] != "proactive_agent" {
		t.Fatalf("input = %+v", input)
	}
	if _, err := createTaskInput(createTaskRequest{SourceType: "unknown"}); err == nil {
		t.Fatal("createTaskInput accepted unknown source")
	}
}
