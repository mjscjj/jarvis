package worldprogress_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/datatypes"
	"jarvis/internal/okrworkspace"
	okrdomain "jarvis/internal/okrworkspace/domain"
	"jarvis/internal/store"
	"jarvis/internal/worldprogress"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
	byPeriod, err := service.ListByPeriod(context.Background(), "2026-W36")
	if err != nil || len(byPeriod) != 1 || byPeriod[0].ID != created.ID {
		t.Fatalf("ListByPeriod() = %#v, %v", byPeriod, err)
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

func TestOKRHierarchyWorldProgressStaysSeparateFromFormalProgress(t *testing.T) {
	okrDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "okr.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := okrworkspace.Migrate(okrDB); err != nil {
		t.Fatal(err)
	}
	objective := okrdomain.Objective{ID: "o-integration", Quarter: "2026-Q3", Title: "完整世界模型"}
	kr := okrdomain.KR{ID: "kr-integration", ObjectiveID: objective.ID, Title: "建立 OKR 到世界的映射"}
	point := okrdomain.KRPoint{ID: "point-integration", KRID: kr.ID, Kind: okrdomain.PointKindStrategy, Title: "完成三层进展展示"}
	week := okrdomain.WeeklyReportWeek{Quarter: objective.Quarter, Week: "2026-W36", TemplateKey: okrdomain.WeekTemplateClassic, OpenedBy: "human"}
	formal := okrdomain.KRProgress{ID: "formal-integration", PointID: point.ID, Week: week.Week, Status: okrdomain.StatusInProgress, Text: "人工填写的正式进展", Source: "manual"}
	for _, row := range []any{&objective, &kr, &point, &week, &formal} {
		if err := okrDB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := okrworkspace.NewService(okrDB)
	if err != nil {
		t.Fatal(err)
	}

	runtimeDB, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(runtimeDB) })
	if err := store.Migrate(runtimeDB); err != nil {
		t.Fatal(err)
	}
	assessments, err := worldprogress.NewService(runtimeDB, func(ctx context.Context, subjectType, subjectID string) error {
		exists, err := workspace.CoreSubjectExists(ctx, subjectType, subjectID)
		if err != nil {
			return err
		}
		if !exists {
			return worldprogress.ErrNotFound
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	zero := int32(0)
	evidenceUntil := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for _, subject := range []struct {
		typeName string
		id       string
		summary  string
	}{
		{"okr_objective", objective.ID, "O 的世界进展"},
		{"okr_kr", kr.ID, "KR 的世界进展"},
		{"okr_point", point.ID, "子 KR 的世界进展"},
	} {
		if _, err := assessments.Create(t.Context(), worldprogress.CreateInput{
			ExpectedVersion: &zero, SubjectType: subject.typeName, SubjectID: subject.id, PeriodKey: week.Week,
			Signal: "yellow", Summary: subject.summary, Evidence: datatypes.JSON(`{"refs":["fact:1"]}`), EvidenceUntil: &evidenceUntil,
		}); err != nil {
			t.Fatalf("create %s world progress: %v", subject.typeName, err)
		}
	}

	views, err := assessments.ListByPeriod(t.Context(), week.Week)
	if err != nil || len(views) != 3 {
		t.Fatalf("ListByPeriod() = %#v, %v", views, err)
	}
	board, err := workspace.ProgressBoard(t.Context(), objective.Quarter, week.Week)
	if err != nil {
		t.Fatal(err)
	}
	entries := board.Objectives[0].KRs[0].Points[0].Entries
	if len(entries) != 1 || entries[0].Text != formal.Text || entries[0].Source != "manual" {
		t.Fatalf("formal progress changed after world assessments: %#v", entries)
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
