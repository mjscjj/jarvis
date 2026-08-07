package background

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"
)

func TestKeyMatterInputValidation(t *testing.T) {
	if err := (&KeyMatterInput{Title: "  "}).validate(); err == nil {
		t.Fatal("blank title accepted")
	}
	if err := (&KeyMatterInput{Title: "法务口径", Status: "任何自由文本都可以"}).validate(); err != nil {
		t.Fatalf("free-text status rejected: %v", err)
	}
}

func TestKeyMatterLifecycleAndFacts(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project := domain.Project{Name: "Jarvis", Role: "owner", Status: "active", Priority: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	service, err := NewKeyMatterService(db)
	if err != nil {
		t.Fatalf("NewKeyMatterService() error = %v", err)
	}
	ctx := context.Background()
	createdAt := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return createdAt }
	initialSummary := "等待法务给出第一版意见"
	created, err := service.Create(ctx, KeyMatterInput{
		Title: "对齐合规口径", Status: "等法务回复", Summary: &initialSummary, ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Project == nil || created.Project.ID != project.ID || created.LastProgressAt != nil || !created.LastActiveAt.Equal(createdAt) {
		t.Fatalf("Create() = %+v, unexpected", created)
	}

	progressSummary := "法务已确认国内口径，等待海外意见"
	progressed, err := service.Update(ctx, created.ID, KeyMatterInput{
		Title: created.Title, Status: created.Status, Summary: &progressSummary, ProjectID: created.ProjectID,
	})
	if err != nil {
		t.Fatalf("Update() summary error = %v", err)
	}
	if progressed.LastProgressAt == nil {
		t.Fatal("summary change did not set last_progress_at")
	}
	progressAt := *progressed.LastProgressAt
	if !progressed.LastActiveAt.Equal(createdAt) {
		t.Fatalf("summary update moved last_active_at = %s", progressed.LastActiveAt)
	}

	updated, err := service.Update(ctx, created.ID, KeyMatterInput{
		Title: created.Title, Status: "本周收口", Summary: &progressSummary, ProjectID: created.ProjectID,
	})
	if err != nil {
		t.Fatalf("Update() status error = %v", err)
	}
	if updated.LastProgressAt == nil || !updated.LastProgressAt.Equal(progressAt) {
		t.Fatalf("status-only update moved last_progress_at from %v to %v", progressAt, updated.LastProgressAt)
	}

	unchanged, err := service.Update(ctx, created.ID, KeyMatterInput{
		Title: updated.Title, Status: updated.Status, Summary: &progressSummary, ProjectID: updated.ProjectID,
	})
	if err != nil {
		t.Fatalf("Update() unchanged error = %v", err)
	}
	if unchanged.LastProgressAt == nil || !unchanged.LastProgressAt.Equal(progressAt) {
		t.Fatalf("unchanged summary moved last_progress_at from %v to %v", progressAt, unchanged.LastProgressAt)
	}
	touchedAt := createdAt.Add(24 * time.Hour)
	service.now = func() time.Time { return touchedAt }
	touched, err := service.Touch(ctx, created.ID)
	if err != nil || !touched.LastActiveAt.Equal(touchedAt) {
		t.Fatalf("Touch() = %+v, error = %v", touched, err)
	}

	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := service.Delete(ctx, created.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Delete() twice error = %v, want ErrInvalidInput", err)
	}
	open, err := service.List(ctx, KeyMatterFilter{ListFilter: ListFilter{Page: 1, PageSize: 20}})
	if err != nil || open.Total != 0 {
		t.Fatalf("List() open = %+v, error = %v", open, err)
	}
	all, err := service.List(ctx, KeyMatterFilter{ListFilter: ListFilter{Page: 1, PageSize: 20}, IncludeClosed: true})
	if err != nil || all.Total != 1 || all.Items[0].ClosedAt == nil {
		t.Fatalf("List() include closed = %+v, error = %v", all, err)
	}

	var facts []domain.Fact
	if err := db.Where("subject_type = ? AND subject_id = ?", "key_matter", created.ID).Order("id ASC").Find(&facts).Error; err != nil {
		t.Fatalf("list key matter facts: %v", err)
	}
	if len(facts) != 4 {
		t.Fatalf("fact count = %d, want 4: %+v", len(facts), facts)
	}
	for _, fact := range facts {
		if fact.SourceKind == nil || *fact.SourceKind != factSourceBackground {
			t.Fatalf("fact source = %#v, want background", fact.SourceKind)
		}
	}
	for i, want := range []string{"立项关键事项", "当前进展更新", "更新关键事项资料：status", "已闭环"} {
		if !strings.Contains(facts[i].Description, want) {
			t.Fatalf("fact[%d] = %q, want contains %q", i, facts[i].Description, want)
		}
	}
}

func TestKeyMatterCapacityAndActivityOrder(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	service, err := NewKeyMatterService(db)
	if err != nil {
		t.Fatalf("NewKeyMatterService() error = %v", err)
	}
	base := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	created := make([]*KeyMatterView, 0, maxOpenKeyMatters)
	for i := 0; i < maxOpenKeyMatters; i++ {
		activeAt := base.Add(time.Duration(i) * time.Hour)
		service.now = func() time.Time { return activeAt }
		item, err := service.Create(t.Context(), KeyMatterInput{Title: fmt.Sprintf("事项 %d", i)})
		if err != nil {
			t.Fatalf("Create(%d) error = %v", i, err)
		}
		created = append(created, item)
	}
	if _, err := service.Create(t.Context(), KeyMatterInput{Title: "超限"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() over capacity error = %v", err)
	}
	list, err := service.List(t.Context(), KeyMatterFilter{ListFilter: ListFilter{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.MaxOpen != maxOpenKeyMatters || list.Items[0].ID != created[len(created)-1].ID {
		t.Fatalf("List() = %+v", list)
	}
	if err := service.Delete(t.Context(), created[0].ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := service.Create(t.Context(), KeyMatterInput{Title: "补位"}); err != nil {
		t.Fatalf("Create() after close error = %v", err)
	}
	if _, err := service.Touch(t.Context(), created[0].ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Touch() closed error = %v", err)
	}
}

func TestKeyMatterActivityOrderNormalizesTimezoneOffsets(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	service, err := NewKeyMatterService(db)
	if err != nil {
		t.Fatalf("NewKeyMatterService() error = %v", err)
	}
	local, err := service.Create(t.Context(), KeyMatterInput{Title: "本地时区事项"})
	if err != nil {
		t.Fatalf("Create(local) error = %v", err)
	}
	utc, err := service.Create(t.Context(), KeyMatterInput{Title: "UTC 事项"})
	if err != nil {
		t.Fatalf("Create(utc) error = %v", err)
	}
	if err := db.Exec("UPDATE key_matter SET last_active_at = ? WHERE id = ?", "2026-08-07T04:24:25+08:00", local.ID).Error; err != nil {
		t.Fatalf("set local activity: %v", err)
	}
	if err := db.Exec("UPDATE key_matter SET last_active_at = ? WHERE id = ?", "2026-08-07T01:31:23Z", utc.ID).Error; err != nil {
		t.Fatalf("set UTC activity: %v", err)
	}
	list, err := service.List(t.Context(), KeyMatterFilter{ListFilter: ListFilter{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.Items[0].ID != utc.ID {
		t.Fatalf("first key matter id = %d, want UTC item %d", list.Items[0].ID, utc.ID)
	}
}

func TestKeyMatterRejectsMissingProject(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	service, err := NewKeyMatterService(db)
	if err != nil {
		t.Fatalf("NewKeyMatterService() error = %v", err)
	}
	missing := uint64(99)
	if _, err := service.Create(t.Context(), KeyMatterInput{Title: "事项", ProjectID: &missing}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() missing project error = %v, want ErrInvalidInput", err)
	}
}
