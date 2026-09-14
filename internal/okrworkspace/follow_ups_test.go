package okrworkspace

import (
	"errors"
	"testing"

	"jarvis/internal/datatypes"
	"jarvis/internal/okrworkspace/domain"
)

func TestFollowUpLifecycleIsScopedVersionedAndIdempotent(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W36", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	input := FollowUpInput{
		ID: "followup-source-row", ExpectedVersion: 0, Quarter: "2026-Q3", Week: "2026-W36",
		Topic: "对齐 AI 计划", Owners: []domain.FollowUpOwner{{Email: "owner@example.test", Name: "张月仁"}},
		Status: domain.FollowUpStatusInProgress, AssignDate: "2026-07-07", Update: "已约会",
		SourceKey: "lark:doc:row", SourcePayload: datatypes.JSON(`{"revision":1}`), SortOrder: 2,
	}
	created, err := service.CreateFollowUp(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 0 || created.Owners[0].Name != "张月仁" {
		t.Fatalf("created = %+v", created)
	}
	again, err := service.CreateFollowUp(t.Context(), input)
	if err != nil || again.ID != created.ID {
		t.Fatalf("idempotent create = %+v, %v", again, err)
	}

	input.ExpectedVersion = created.Version
	input.Status = domain.FollowUpStatusDone
	updated, err := service.UpdateFollowUp(t.Context(), created.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 1 || updated.Status != domain.FollowUpStatusDone {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := service.UpdateFollowUp(t.Context(), created.ID, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v, want ErrConflict", err)
	}
	list, err := service.FollowUps(t.Context(), "2026-Q3", "2026-W36")
	if err != nil || list.Count != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestFollowUpAcceptsAbandonedStatus(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W36", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateFollowUp(t.Context(), FollowUpInput{
		ID: "followup-abandoned", Quarter: "2026-Q3", Week: "2026-W36", Topic: "不再推进",
		Status: domain.FollowUpStatusAbandoned, SourcePayload: datatypes.JSON(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != domain.FollowUpStatusAbandoned {
		t.Fatalf("status = %q, want %q", created.Status, domain.FollowUpStatusAbandoned)
	}
}

func TestFollowUpRejectsUnknownStatusAndUnresolvedOwners(t *testing.T) {
	service, err := NewService(openWorkspaceTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	input := FollowUpInput{
		ID: "followup-invalid", Quarter: "2026-Q3", Week: "2026-W36", Topic: "事项",
		Status: domain.FollowUpStatus("doing"), Owners: []domain.FollowUpOwner{{Name: "只有名字"}}, SourcePayload: datatypes.JSON(`{}`),
	}
	if _, err := service.CreateFollowUp(t.Context(), input); err == nil {
		t.Fatal("invalid follow-up unexpectedly created")
	}
}

func TestFollowUpsOrderByAssignDateWithEmptyDatesLast(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W36", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []FollowUpInput{
		{ID: "empty", Quarter: "2026-Q3", Week: "2026-W36", Topic: "无日期", Status: domain.FollowUpStatusNotStarted, SourcePayload: datatypes.JSON(`{}`), SortOrder: 0},
		{ID: "later", Quarter: "2026-Q3", Week: "2026-W36", Topic: "较晚", Status: domain.FollowUpStatusNotStarted, AssignDate: "2026-08-25", SourcePayload: datatypes.JSON(`{}`), SortOrder: 1},
		{ID: "same-date-second", Quarter: "2026-Q3", Week: "2026-W36", Topic: "同日第二条", Status: domain.FollowUpStatusNotStarted, AssignDate: "2026-08-04", SourcePayload: datatypes.JSON(`{}`), SortOrder: 3},
		{ID: "earlier", Quarter: "2026-Q3", Week: "2026-W36", Topic: "较早", Status: domain.FollowUpStatusNotStarted, AssignDate: "2026-07-07", SourcePayload: datatypes.JSON(`{}`), SortOrder: 4},
		{ID: "same-date-first", Quarter: "2026-Q3", Week: "2026-W36", Topic: "同日第一条", Status: domain.FollowUpStatusNotStarted, AssignDate: "2026-08-04", SourcePayload: datatypes.JSON(`{}`), SortOrder: 2},
	} {
		if _, err := service.CreateFollowUp(t.Context(), input); err != nil {
			t.Fatal(err)
		}
	}
	list, err := service.FollowUps(t.Context(), "2026-Q3", "2026-W36")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"earlier", "same-date-first", "same-date-second", "later", "empty"}
	if len(list.Items) != len(want) {
		t.Fatalf("items = %#v", list.Items)
	}
	for index, id := range want {
		if list.Items[index].ID != id {
			t.Fatalf("item %d = %q, want %q; full list = %#v", index, list.Items[index].ID, id, list.Items)
		}
	}
}
