package background

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/store"
)

func TestResourceActivityOrderAndCapacity(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	service, err := NewResourceService(db)
	if err != nil {
		t.Fatalf("NewResourceService() error = %v", err)
	}
	base := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	created := make([]*ResourceView, 0, maxActiveManagedResources)
	for i := 0; i < maxActiveManagedResources; i++ {
		activeAt := base.Add(time.Duration(i) * time.Minute)
		service.now = func() time.Time { return activeAt }
		item, err := service.Create(t.Context(), ResourceInput{Title: fmt.Sprintf("资源 %d", i), ResourceType: "doc"})
		if err != nil {
			t.Fatalf("Create(%d) error = %v", i, err)
		}
		created = append(created, item)
	}
	if _, err := service.Create(t.Context(), ResourceInput{Title: "超限", ResourceType: "doc"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() over capacity error = %v", err)
	}
	inactive := false
	spare, err := service.Create(t.Context(), ResourceInput{Title: "停用资源", ResourceType: "doc", IsActive: &inactive})
	if err != nil {
		t.Fatalf("Create() inactive error = %v", err)
	}
	active := true
	if _, err := service.Update(t.Context(), spare.ID, ResourceInput{Title: spare.Title, ResourceType: spare.ResourceType, IsActive: &active}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Update() activate over capacity error = %v", err)
	}
	if _, err := service.Touch(t.Context(), spare.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Touch() inactive error = %v", err)
	}
	if _, err := service.Update(t.Context(), created[0].ID, ResourceInput{Title: "资源 0 更新", ResourceType: "doc"}); err != nil {
		t.Fatalf("Update() active resource error = %v", err)
	}
	updated, err := service.Get(t.Context(), created[0].ID)
	if err != nil || !updated.LastActiveAt.Equal(created[0].LastActiveAt) {
		t.Fatalf("ordinary update moved activity: resource=%+v error=%v", updated, err)
	}
	if _, err := service.Update(t.Context(), created[0].ID, ResourceInput{Title: updated.Title, ResourceType: updated.ResourceType, IsActive: &inactive}); err != nil {
		t.Fatalf("Update() deactivate error = %v", err)
	}
	touchedAt := base.Add(72 * time.Hour)
	service.now = func() time.Time { return touchedAt }
	reactivated, err := service.Update(t.Context(), spare.ID, ResourceInput{Title: spare.Title, ResourceType: spare.ResourceType, IsActive: &active})
	if err != nil || !reactivated.LastActiveAt.Equal(touchedAt) {
		t.Fatalf("Update() reactivate = %+v, error = %v", reactivated, err)
	}
	touchAgain := touchedAt.Add(time.Hour)
	service.now = func() time.Time { return touchAgain }
	touched, err := service.Touch(t.Context(), spare.ID)
	if err != nil || !touched.LastActiveAt.Equal(touchAgain) {
		t.Fatalf("Touch() = %+v, error = %v", touched, err)
	}
	list, err := service.List(t.Context(), ResourceFilter{ListFilter: ListFilter{Page: 1, PageSize: 100}})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.ActiveTotal != maxActiveManagedResources || list.MaxActive != maxActiveManagedResources || list.Items[0].ID != spare.ID {
		t.Fatalf("List() = %+v", list)
	}
}

func TestResourceActivityOrderNormalizesTimezoneOffsets(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	service, err := NewResourceService(db)
	if err != nil {
		t.Fatalf("NewResourceService() error = %v", err)
	}
	local, err := service.Create(t.Context(), ResourceInput{Title: "本地时区资源", ResourceType: "doc"})
	if err != nil {
		t.Fatalf("Create(local) error = %v", err)
	}
	utc, err := service.Create(t.Context(), ResourceInput{Title: "UTC 资源", ResourceType: "doc"})
	if err != nil {
		t.Fatalf("Create(utc) error = %v", err)
	}
	if err := db.Exec("UPDATE managed_resource SET last_active_at = ? WHERE id = ?", "2026-08-07T04:24:25+08:00", local.ID).Error; err != nil {
		t.Fatalf("set local activity: %v", err)
	}
	if err := db.Exec("UPDATE managed_resource SET last_active_at = ? WHERE id = ?", "2026-08-07T01:31:23Z", utc.ID).Error; err != nil {
		t.Fatalf("set UTC activity: %v", err)
	}
	list, err := service.List(t.Context(), ResourceFilter{ListFilter: ListFilter{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.Items[0].ID != utc.ID {
		t.Fatalf("first resource id = %d, want UTC item %d", list.Items[0].ID, utc.ID)
	}
}
