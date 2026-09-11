package scheduledtask

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/store"
)

func TestPluginSchedulesReuseTaskDispatchAndPauseWithoutDeletingHistory(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	submitter := &okrProgressSubmitter{}
	svc, err := NewService(db, submitter, okrProgressResumer{}, 10, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	pluginEnabled := true
	skillEnabled := true
	svc.SetSkillGate(func(_ context.Context, name string) (bool, error) {
		if name != "product-prd-review" {
			return false, errors.New("unknown skill")
		}
		return skillEnabled, nil
	})
	svc.SetPluginGate(func(_ context.Context, id string) (bool, error) {
		if id != "product-management" {
			return false, errors.New("unknown plugin")
		}
		return pluginEnabled, nil
	})
	interval := 10
	owned, err := svc.Create(t.Context(), Input{
		Title: "Review PRD", Instruction: "使用背景指定的 Skill 阅读指定文档", ActionType: "agent_task",
		ContextSnapshot: json.RawMessage(`{"plugin":"product-management","skill":"product-prd-review","document":"https://example.com/prd"}`),
		ScheduleType:    "interval", IntervalMinutes: &interval,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Create(t.Context(), Input{Title: "ordinary", Instruction: "other work", ScheduleType: "interval", IntervalMinutes: &interval})
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := svc.List(t.Context(), ListFilter{PluginID: "product-management", Limit: 1})
	if err != nil || len(filtered) != 1 || filtered[0].ID != owned.ID {
		t.Fatalf("filter=%+v err=%v", filtered, err)
	}
	all, err := svc.List(t.Context(), ListFilter{Limit: 100})
	if err != nil || len(all) != 2 {
		t.Fatalf("global list=%+v err=%v", all, err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := svc.RunDue(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunDue(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(submitter.inputs) != 2 {
		t.Fatalf("expected one Task per occurrence: %+v", submitter.inputs)
	}
	var background map[string]any
	if err := json.Unmarshal(submitter.inputs[0].Background, &background); err != nil {
		t.Fatal(err)
	}
	if background["skill"] != "product-prd-review" || background["document"] != "https://example.com/prd" {
		t.Fatalf("lost scope: %+v", background)
	}
	pluginEnabled = false
	paused, err := svc.Trigger(t.Context(), owned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(submitter.inputs) != 2 || paused.LastTaskID == nil || *paused.LastTaskID != 77 || !strings.Contains(*paused.LastResult, "已关闭") {
		t.Fatalf("pause lost history or dispatched: %+v", paused)
	}
	pluginEnabled = true
	skillEnabled = false
	if _, err := svc.Trigger(t.Context(), owned.ID); err != nil {
		t.Fatal(err)
	}
	if len(submitter.inputs) != 2 {
		t.Fatal("disabled skill created Task")
	}
	skillEnabled = true
	if _, err := svc.Trigger(t.Context(), owned.ID); err != nil {
		t.Fatal(err)
	}
	if len(submitter.inputs) != 3 {
		t.Fatalf("expected resumed dispatch: %d", len(submitter.inputs))
	}
}
