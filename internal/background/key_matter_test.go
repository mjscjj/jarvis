package background

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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
	initialSummary := "等待法务给出第一版意见"
	created, err := service.Create(ctx, KeyMatterInput{
		Title: "对齐合规口径", Status: "等法务回复", Summary: &initialSummary, ProjectID: &project.ID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Project == nil || created.Project.ID != project.ID || created.LastProgressAt != nil {
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
