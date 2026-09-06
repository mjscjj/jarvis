package worldprogress_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/datatypes"
	"jarvis/internal/store"
	"jarvis/internal/worldprogress"
)

func TestServiceCreateReadUpdateAndNoop(t *testing.T) {
	service := newService(t, func(_ context.Context, subjectType, subjectID string) error {
		if subjectType != "okr_point" || subjectID != "point-1" {
			return worldprogress.ErrNotFound
		}
		return nil
	})
	zero := int32(0)
	evidenceUntil := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	created, err := service.Create(context.Background(), worldprogress.CreateInput{
		ExpectedVersion: &zero, SubjectType: " OKR_POINT ", SubjectID: " point-1 ", PeriodKey: "2026-W36",
		Signal: "YELLOW", Summary: " 客户 C 尚未启动。 ",
		Evidence:      datatypes.JSON(`{"refs":["fact:12"],"observed":[{"ref":"okr_progress:p1","version":3}]}`),
		EvidenceUntil: &evidenceUntil,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Version != 0 || created.Signal != "yellow" || created.Summary != "客户 C 尚未启动。" {
		t.Fatalf("created = %#v", created)
	}
	byKey, err := service.GetBySubjectPeriod(context.Background(), worldprogress.Filter{
		SubjectType: "okr_point", SubjectID: "point-1", PeriodKey: "2026-W36",
	})
	if err != nil || byKey.ID != created.ID {
		t.Fatalf("GetBySubjectPeriod() = %#v, %v", byKey, err)
	}

	version := created.Version
	unchanged, err := service.Update(context.Background(), created.ID, worldprogress.UpdateInput{
		ExpectedVersion: &version, Signal: created.Signal, Summary: created.Summary,
		Evidence: datatypes.JSON(created.Evidence), EvidenceUntil: &evidenceUntil,
	})
	if err != nil {
		t.Fatalf("no-op Update() error = %v", err)
	}
	if unchanged.Version != 0 || !unchanged.AssessedAt.Equal(created.AssessedAt) {
		t.Fatalf("no-op changed assessment = %#v", unchanged)
	}

	later := evidenceUntil.Add(2 * time.Hour)
	updated, err := service.Update(context.Background(), created.ID, worldprogress.UpdateInput{
		ExpectedVersion: &version, Signal: "red", Summary: "客户 C 阻塞。",
		Evidence: datatypes.JSON(`{"refs":["fact:13"]}`), EvidenceUntil: &later,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 1 || updated.Signal != "red" || !updated.EvidenceUntil.Equal(later) {
		t.Fatalf("updated = %#v", updated)
	}
	if _, err := service.Update(context.Background(), created.ID, worldprogress.UpdateInput{
		ExpectedVersion: &version, Signal: "green", Summary: "stale",
		Evidence: datatypes.JSON(`{}`), EvidenceUntil: &later,
	}); !errors.Is(err, worldprogress.ErrConflict) {
		t.Fatalf("stale Update() error = %v, want conflict", err)
	}
}

func TestServiceRejectsDuplicateInvalidEvidenceAndUnavailableSubject(t *testing.T) {
	available := true
	service := newService(t, func(_ context.Context, _, _ string) error {
		if !available {
			return worldprogress.ErrSubjectUnavailable
		}
		return nil
	})
	zero := int32(0)
	now := time.Now().UTC()
	valid := worldprogress.CreateInput{
		ExpectedVersion: &zero, SubjectType: "okr_point", SubjectID: "point-1", PeriodKey: "2026-W36",
		Signal: "green", Summary: "有证据。", Evidence: datatypes.JSON(`{"refs":["fact:1"]}`), EvidenceUntil: &now,
	}
	created, err := service.Create(context.Background(), valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), valid); !errors.Is(err, worldprogress.ErrConflict) {
		t.Fatalf("duplicate Create() error = %v", err)
	}
	invalid := valid
	invalid.SubjectID = "point-2"
	invalid.Evidence = datatypes.JSON(`{"refs":["not a ref"]}`)
	if _, err := service.Create(context.Background(), invalid); !errors.Is(err, worldprogress.ErrInvalidInput) {
		t.Fatalf("invalid evidence error = %v", err)
	}
	available = false
	version := created.Version
	if _, err := service.Update(context.Background(), created.ID, worldprogress.UpdateInput{
		ExpectedVersion: &version, Signal: "yellow", Summary: "new", Evidence: datatypes.JSON(`{}`), EvidenceUntil: &now,
	}); !errors.Is(err, worldprogress.ErrSubjectUnavailable) {
		t.Fatalf("disabled subject update error = %v", err)
	}
	if _, err := service.Get(context.Background(), created.ID); err != nil {
		t.Fatalf("historical read while subject unavailable: %v", err)
	}
}

func newService(t *testing.T, validator worldprogress.SubjectValidator) *worldprogress.Service {
	t.Helper()
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	service, err := worldprogress.NewService(db, validator)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
