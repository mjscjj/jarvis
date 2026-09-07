package okrworkspace

import (
	"path/filepath"
	"testing"
	"time"
)

func TestActivityStoreAppendsAndFiltersNewestFirst(t *testing.T) {
	store, err := NewActivityStore(filepath.Join(t.TempDir(), "activity"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []ActivityEntry{
		{At: time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC), Surface: "plan", PlanID: "plan-1", Action: "plan_saved", Summary: "保存 Plan"},
		{At: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), Surface: "weekly", Quarter: "2026-Q3", Week: "2026-W36", Action: "progress_created", Summary: "新增\n进展"},
		{At: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), Surface: "weekly", Quarter: "2026-Q3", Week: "2026-W37", Action: "progress_created", Summary: "另一周"},
	} {
		if err := store.Append(entry); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.List(ActivityQuery{Surface: "weekly", Quarter: "2026-Q3", Week: "2026-W36", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Summary != "新增 进展" {
		t.Fatalf("entries = %+v", entries)
	}
	all, err := store.List(ActivityQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Week != "2026-W37" || all[1].Week != "2026-W36" {
		t.Fatalf("newest entries = %+v", all)
	}
}
