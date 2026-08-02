//go:build integration

// This file exercises seed flows against an isolated SQLite database.

package background

import (
	"context"
	"path/filepath"
	"testing"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/larkcli"
	"jarvis/internal/store"
)

// stubMemberLister returns canned members per chat id for the person import
// test, so no real lark-cli call is made.
type stubMemberLister struct {
	byChat map[string][]larkcli.ChatMember
}

func (s *stubMemberLister) ListChatMembers(_ context.Context, chatID string) ([]larkcli.ChatMember, error) {
	return s.byChat[chatID], nil
}

// TestSeedIdempotentSQLite verifies the one-shot seed creates the inferred
// project/task backgrounds once and creates nothing on a re-run.
func TestSeedIdempotentSQLite(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	ctx := context.Background()
	first, err := Seed(ctx, db)
	if err != nil {
		t.Fatalf("Seed() first run error = %v", err)
	}
	if first.ProjectsCreated != len(seedProjects) {
		t.Fatalf("first run ProjectsCreated = %d, want %d", first.ProjectsCreated, len(seedProjects))
	}
	if first.TasksCreated != len(seedTasks) {
		t.Fatalf("first run TasksCreated = %d, want %d", first.TasksCreated, len(seedTasks))
	}

	second, err := Seed(ctx, db)
	if err != nil {
		t.Fatalf("Seed() second run error = %v", err)
	}
	if second.ProjectsCreated != 0 || second.TasksCreated != 0 {
		t.Fatalf("second run created rows: projects=%d tasks=%d, want 0/0 (not idempotent)", second.ProjectsCreated, second.TasksCreated)
	}
	if second.ProjectsSkipped != len(seedProjects) || second.TasksSkipped != len(seedTasks) {
		t.Fatalf("second run skipped: projects=%d tasks=%d, want %d/%d", second.ProjectsSkipped, second.TasksSkipped, len(seedProjects), len(seedTasks))
	}
}

// TestSeedPersonsFromKeyGroupsSQLite verifies the group-member import dedups
// across groups, skips already-present persons (by open_id), and is idempotent.
func TestSeedPersonsFromKeyGroupsSQLite(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	ctx := context.Background()
	keyGroup := domain.Group{ChatID: "oc_seed_test", ChatMode: "group", IsKeyGroup: true}
	if err := db.WithContext(ctx).Create(&keyGroup).Error; err != nil {
		t.Fatalf("create key group error = %v", err)
	}
	t.Cleanup(func() {
		db.WithContext(ctx).Where("open_id IN ?", []string{"ou_a", "ou_b"}).Delete(&domain.Person{})
		db.WithContext(ctx).Delete(&domain.Group{}, keyGroup.ID)
	})

	lister := &stubMemberLister{byChat: map[string][]larkcli.ChatMember{
		"oc_seed_test": {
			{MemberID: "ou_a", Name: "张三"},
			{MemberID: "ou_b", Name: "李四"},
			{MemberID: "ou_a", Name: "张三重复"}, // dedup within group
		},
	}}

	first, err := SeedPersonsFromKeyGroups(ctx, db, lister)
	if err != nil {
		t.Fatalf("SeedPersonsFromKeyGroups() first error = %v", err)
	}
	if first.PersonsAdded < 2 {
		t.Fatalf("first run PersonsAdded = %d, want >= 2", first.PersonsAdded)
	}

	assertActive := func(openID string) {
		var got domain.Person
		if err := db.WithContext(ctx).Where("open_id = ?", openID).First(&got).Error; err != nil {
			t.Fatalf("lookup %q error = %v", openID, err)
		}
		if got.Role != "colleague" || !got.IsActive {
			t.Fatalf("imported %q = role %q active %v, want colleague/true", openID, got.Role, got.IsActive)
		}
	}
	assertActive("ou_a")
	assertActive("ou_b")

	second, err := SeedPersonsFromKeyGroups(ctx, db, lister)
	if err != nil {
		t.Fatalf("SeedPersonsFromKeyGroups() second error = %v", err)
	}
	if second.PersonsAdded != 0 {
		t.Fatalf("second run PersonsAdded = %d, want 0 (not idempotent)", second.PersonsAdded)
	}
}
