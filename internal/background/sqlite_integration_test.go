//go:build integration

package background

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"

	"strconv"
)

// TestBackgroundCRUDSQLite exercises Project/Person CRUD and Group background
// patching against an isolated SQLite database.
func TestBackgroundCRUDSQLite(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(db); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	ctx := context.Background()
	projects, err := NewProjectService(db)
	if err != nil {
		t.Fatalf("NewProjectService() error = %v", err)
	}
	persons, err := NewPersonService(db)
	if err != nil {
		t.Fatalf("NewPersonService() error = %v", err)
	}
	groups, err := NewGroupBackgroundService(db, nil)
	if err != nil {
		t.Fatalf("NewGroupBackgroundService() error = %v", err)
	}
	keyMatters, err := NewKeyMatterService(db)
	if err != nil {
		t.Fatalf("NewKeyMatterService() error = %v", err)
	}

	suffix := time.Now().UnixNano()

	t.Run("project lifecycle", func(t *testing.T) {
		created, err := projects.Create(ctx, ProjectInput{
			Name: "IntegrationProject", Role: "owner", Status: "active", Priority: 2,
			Repos: json.RawMessage(`["repo-a"]`),
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if created.ID == 0 {
			t.Fatal("Create() returned zero ID")
		}
		t.Cleanup(func() { _ = projects.Delete(ctx, created.ID) })

		got, err := projects.Get(ctx, created.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.Name != "IntegrationProject" || got.Priority != 2 {
			t.Fatalf("Get() = %+v, unexpected", got)
		}

		updated, err := projects.Update(ctx, created.ID, ProjectInput{
			Name: "IntegrationProjectV2", Role: "participant", Status: "paused", Priority: 4,
		})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if updated.Name != "IntegrationProjectV2" || updated.Status != "paused" || updated.Priority != 4 {
			t.Fatalf("Update() = %+v, unexpected", updated)
		}
		// JSON column cleared on update when omitted.
		if len(updated.Repos) != 0 && string(updated.Repos) != "null" {
			t.Fatalf("Update() repos = %q, want cleared", string(updated.Repos))
		}

		list, err := projects.List(ctx, ListFilter{Page: 1, PageSize: 50})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if list.Total < 1 {
			t.Fatalf("List() total = %d, want >= 1", list.Total)
		}

		if err := projects.Delete(ctx, created.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		archived, err := projects.Get(ctx, created.ID)
		if err != nil || archived.Status != "archived" {
			t.Fatalf("Get() after archive = %#v, error = %v", archived, err)
		}
		if err := projects.Delete(ctx, created.ID); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Delete() twice error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("key matter table and fact side effects", func(t *testing.T) {
		created, err := keyMatters.Create(ctx, KeyMatterInput{Title: "IntegrationMatter", Status: "跟进中"})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if !db.Migrator().HasTable(&domain.KeyMatter{}) {
			t.Fatal("key_matter table was not migrated")
		}
		summary := "完成第一轮对齐"
		if _, err := keyMatters.Update(ctx, created.ID, KeyMatterInput{Title: created.Title, Status: created.Status, Summary: &summary}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if err := keyMatters.Delete(ctx, created.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		var count int64
		if err := db.Model(&domain.Fact{}).Where("subject_type = ? AND subject_id = ? AND source_kind = ?", "key_matter", created.ID, factSourceBackground).Count(&count).Error; err != nil {
			t.Fatalf("count key matter facts: %v", err)
		}
		if count != 3 {
			t.Fatalf("key matter fact count = %d, want 3", count)
		}
	})

	t.Run("person lifecycle", func(t *testing.T) {
		openID := "ou_integration_" + itoa(suffix)
		created, err := persons.Create(ctx, PersonCreateInput{
			OpenID:            openID,
			PersonUpdateInput: PersonUpdateInput{Name: "IntegrationLeader", Role: "leader", PriorityWeight: 0.95},
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		t.Cleanup(func() { _ = persons.Delete(ctx, created.ID) })
		if !created.IsActive {
			t.Fatal("Create() default IsActive = false, want true")
		}

		inactive := false
		updated, err := persons.Update(ctx, created.ID, PersonUpdateInput{
			Name: "IntegrationLeader", Role: "key", PriorityWeight: 0.5, IsActive: &inactive,
		})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if updated.Role != "key" || updated.IsActive {
			t.Fatalf("Update() = %+v, unexpected", updated)
		}
		if updated.OpenID != openID {
			t.Fatalf("Update() open_id = %q, want preserved %q", updated.OpenID, openID)
		}

		if err := persons.Delete(ctx, created.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
	})

	t.Run("group background patch only", func(t *testing.T) {
		// A group is created by capture; here we insert one directly to represent
		// a discovered chat, then verify UpdateBackground touches only curated fields.
		chatID := "oc_integration_" + itoa(suffix)
		discoveredName := "DiscoveredChatName"
		seed := domain.Group{
			ChatID: chatID, ChatMode: "group", Name: &discoveredName, Tier: "cold",
		}
		if err := db.WithContext(ctx).Create(&seed).Error; err != nil {
			t.Fatalf("seed group error = %v", err)
		}
		t.Cleanup(func() { db.Unscoped().Delete(&domain.Group{}, seed.ID) })

		project, err := projects.Create(ctx, ProjectInput{
			Name: "GroupOwnerProject", Role: "owner", Status: "active", Priority: 3,
		})
		if err != nil {
			t.Fatalf("Create() owner project error = %v", err)
		}
		t.Cleanup(func() { _ = projects.Delete(ctx, project.ID) })

		backgroundNote := "人工维护的会话背景"
		updated, err := groups.UpdateBackground(ctx, seed.ID, GroupBackgroundInput{
			BackgroundNote: &backgroundNote,
			ProjectID:      &project.ID, RelatedGroup: true, Pinned: true, IncludeInMemory: true, IsKeyGroup: true,
		})
		if err != nil {
			t.Fatalf("UpdateBackground() error = %v", err)
		}
		if updated.ProjectID == nil || *updated.ProjectID != project.ID {
			t.Fatalf("UpdateBackground() project_id = %v, want %d", updated.ProjectID, project.ID)
		}
		if !updated.RelatedGroup || !updated.IsKeyGroup || !updated.Pinned {
			t.Fatalf("UpdateBackground() curated flags = %+v, unexpected", updated)
		}
		if updated.BackgroundNote == nil || *updated.BackgroundNote != "人工维护的会话背景" {
			t.Fatalf("UpdateBackground() background_note = %v, want curated value", updated.BackgroundNote)
		}
		// Capture-owned columns must be untouched.
		if updated.ChatID != chatID || updated.Name == nil || *updated.Name != discoveredName || updated.Tier != "cold" {
			t.Fatalf("UpdateBackground() mutated capture columns: chat_id=%q name=%v tier=%q", updated.ChatID, updated.Name, updated.Tier)
		}

		// Non-existent group id → ErrNotFound.
		if _, err := groups.UpdateBackground(ctx, 1<<62, GroupBackgroundInput{}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("UpdateBackground() missing id error = %v, want ErrNotFound", err)
		}
		// Non-existent project_id → ErrInvalidInput.
		bogus := uint64(1 << 62)
		if _, err := groups.UpdateBackground(ctx, seed.ID, GroupBackgroundInput{ProjectID: &bogus}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("UpdateBackground() bogus project_id error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("group keyword search spans owner/project/description", func(t *testing.T) {
		token := "kwsearch" + itoa(suffix)
		ownerID := "ou_owner_" + token
		owner, err := persons.Create(ctx, PersonCreateInput{
			OpenID:            ownerID,
			PersonUpdateInput: PersonUpdateInput{Name: "OwnerPerson" + token, Role: "colleague", PriorityWeight: 0.4},
		})
		if err != nil {
			t.Fatalf("Create() owner person error = %v", err)
		}
		t.Cleanup(func() { _ = persons.Delete(ctx, owner.ID) })

		project, err := projects.Create(ctx, ProjectInput{
			Name: "SearchProject" + token, Role: "owner", Status: "active", Priority: 3,
		})
		if err != nil {
			t.Fatalf("Create() search project error = %v", err)
		}
		t.Cleanup(func() { _ = projects.Delete(ctx, project.ID) })

		// A chat with a NULL name (like a p2p/topic chat) that must still be found
		// via its owner, project, description and chat_id.
		chatID := "oc_kw_" + token
		desc := "DescNeedle" + token
		g := domain.Group{
			ChatID: chatID, ChatMode: "group", OwnerOpenID: &ownerID, Description: &desc,
			ProjectID: &project.ID, RelatedGroup: false, Tier: "cold",
		}
		if err := db.WithContext(ctx).Create(&g).Error; err != nil {
			t.Fatalf("seed searchable group error = %v", err)
		}
		t.Cleanup(func() { db.Unscoped().Delete(&domain.Group{}, g.ID) })

		// Each keyword targets a different joined/own column; all must hit the
		// same chat. RelatedOnly=true is intentional to also prove broadening.
		cases := []struct {
			name    string
			keyword string
		}{
			{"by owner name", "OwnerPerson" + token},
			{"by project name", "SearchProject" + token},
			{"by description", "DescNeedle" + token},
			{"by chat_id", chatID},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				list, err := groups.List(ctx, GroupFilter{
					ListFilter: ListFilter{Page: 1, PageSize: 50}, RelatedOnly: true, Keyword: tc.keyword,
				})
				if err != nil {
					t.Fatalf("List() error = %v", err)
				}
				if !list.Broadened {
					t.Fatalf("List() Broadened = false, want true (keyword should escape related-only)")
				}
				found := false
				for _, item := range list.Items {
					if item.ChatID == chatID {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("List() keyword=%q did not return chat_id=%q (total=%d)", tc.keyword, chatID, list.Total)
				}
			})
		}
	})

	t.Run("validation errors are ErrInvalidInput", func(t *testing.T) {
		if _, err := projects.Create(ctx, ProjectInput{Name: "", Role: "owner", Status: "active", Priority: 1}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Create() blank name error = %v, want ErrInvalidInput", err)
		}
		if _, err := persons.Create(ctx, PersonCreateInput{OpenID: "x", PersonUpdateInput: PersonUpdateInput{Name: "y", Role: "bad", PriorityWeight: 0.5}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Create() bad role error = %v, want ErrInvalidInput", err)
		}
	})
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
