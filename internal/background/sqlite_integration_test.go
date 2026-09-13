package background

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
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
		if _, err := keyMatters.Update(ctx, created.ID, KeyMatterInput{Title: created.Title, Status: "推进中"}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if err := keyMatters.Delete(ctx, created.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		var count int64
		if err := db.Model(&domain.Fact{}).Where("subject_type = ? AND subject_id = ? AND source_kind = ?", "key_matter", created.ID, factSourceBackground).Count(&count).Error; err != nil {
			t.Fatalf("count key matter facts: %v", err)
		}
		if count != 2 {
			t.Fatalf("key matter fact count = %d, want 2", count)
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

		updated, err := groups.UpdateBackground(ctx, seed.ID, GroupBackgroundInput{
			ProjectID: &project.ID, RelatedGroup: true, Pinned: true, IncludeInMemory: true, IsKeyGroup: true,
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

func TestBackgroundListFiltersSQLite(t *testing.T) {
	db := openBackgroundTestDB(t)
	ctx := t.Context()
	text := func(value string) *string { return &value }
	id := func(value uint64) *uint64 { return &value }
	now := time.Now().UTC()
	seed := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed list fixtures: %v", err)
		}
	}
	seed(&[]domain.Project{
		{ID: 1, Code: text(`P_%\`), Name: "ProjectName", Role: "owner", Status: "active", Priority: 1, Summary: text(`needle %_\`)},
		{ID: 2, Code: text(`P_%\-suffix`), Name: "Second", Role: "owner", Status: "active", Priority: 1, Summary: text("needle")},
		{ID: 3, Name: "Third", Role: "owner", Status: "paused", Priority: 1, Summary: text("needle")},
		{ID: 4, Name: "Unrelated", Role: "participant", Status: "planning", Priority: 1},
		{ID: 5, Name: "Archived", Role: "owner", Status: "archived", Priority: 1, Summary: text(`needle %_\`)},
	})
	seed(&[]domain.Person{
		{ID: 1, OpenID: `ou_%\`, Name: "PersonName", EnName: text("EnglishName"), Department: text(`needle %_\`), Title: text("Architect"), Role: "leader", PriorityWeight: 0.5},
		{ID: 2, OpenID: `ou_%\-suffix`, Name: "SecondPerson", Department: text("needle"), Role: "leader", PriorityWeight: 0.5},
		{ID: 3, OpenID: "ou_third", Name: "ThirdPerson", Department: text("needle"), Role: "leader", PriorityWeight: 0.5},
		{ID: 4, OpenID: "ou_other", Name: "OtherPerson", Department: text("needle"), Role: "colleague", PriorityWeight: 0.5},
	})
	seed(&[]domain.KeyMatter{
		{ID: 1, Title: "MatterTitle", Status: "推进中", Summary: text(`needle %_\`), LastActiveAt: now},
		{ID: 2, Title: "SecondMatter", Status: "等待", Summary: text("needle"), LastActiveAt: now},
		{ID: 3, Title: "ThirdMatter", Status: "等待", Summary: text("needle"), LastActiveAt: now},
		{ID: 4, Title: "ClosedMatter", Status: "完成", Summary: text(`needle %_\`), ClosedAt: &now, LastActiveAt: now},
		{ID: 5, Title: "OtherMatter", Status: "等待", LastActiveAt: now},
	})
	seed(&[]domain.ManagedResource{
		{ID: 1, Title: "ResourceTitle", ResourceType: "repo", URL: text("https://example.test/repository"), LocalPath: text("/workspace/source"), Summary: text(`needle %_\`), PersonID: id(1), ProjectID: id(1), LinkPrincipal: true, LastActiveAt: now},
		{ID: 2, Title: "SecondResource", ResourceType: "doc", Summary: text("needle"), PersonID: id(1), ProjectID: id(1), LinkPrincipal: true, LastActiveAt: now},
		{ID: 3, Title: "ThirdResource", ResourceType: "doc", Summary: text("needle"), PersonID: id(1), ProjectID: id(1), LinkPrincipal: true, LastActiveAt: now},
		{ID: 4, Title: "OtherResource", ResourceType: "doc", PersonID: id(2), LastActiveAt: now},
		{ID: 5, Title: "InactiveResource", ResourceType: "doc", Summary: text("needle"), PersonID: id(1), ProjectID: id(1), LinkPrincipal: true, LastActiveAt: now},
	})
	if err := db.Model(&domain.ManagedResource{}).Where("id = ?", 5).UpdateColumn("is_active", false).Error; err != nil {
		t.Fatalf("seed inactive resource: %v", err)
	}
	seed(&[]domain.Group{
		{ID: 1, ChatID: `oc_%\`, ChatMode: "group", Name: text("needle"), Tier: "hot"},
		{ID: 2, ChatID: `oc_%\-suffix`, ChatMode: "group", Name: text("needle"), Tier: "cold", RelatedGroup: true},
	})
	assertIDs := func(t *testing.T, total int64, got, want []uint64, wantTotal int64) {
		t.Helper()
		if total != wantTotal || !reflect.DeepEqual(got, want) {
			t.Fatalf("total=%d ids=%v, want total=%d ids=%v", total, got, wantTotal, want)
		}
	}

	t.Run("projects", func(t *testing.T) {
		svc, err := NewProjectService(db)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name   string
			filter ListFilter
			ids    []uint64
			total  int64
		}{
			{"summary second page", ListFilter{Page: 2, PageSize: 2, Keyword: "needle"}, []uint64{1}, 3},
			{"name", ListFilter{Page: 1, PageSize: 10, Keyword: "ProjectName"}, []uint64{1}, 1},
			{"code keyword", ListFilter{Page: 1, PageSize: 10, Keyword: "P_"}, []uint64{2, 1}, 2},
			{"role keyword", ListFilter{Page: 1, PageSize: 10, Keyword: "participant"}, []uint64{4}, 1},
			{"status keyword", ListFilter{Page: 1, PageSize: 10, Keyword: "paused"}, []uint64{3}, 1},
			{"exact code", ListFilter{Page: 1, PageSize: 10, Code: `P_%\`}, []uint64{1}, 1},
			{"partial code is not exact", ListFilter{Page: 1, PageSize: 10, Code: "P_"}, []uint64{}, 0},
			{"combined filters", ListFilter{Page: 1, PageSize: 10, Keyword: "paused", Code: `P_%\`}, []uint64{}, 0},
			{"empty", ListFilter{Page: 1, PageSize: 10, Keyword: "missing"}, []uint64{}, 0},
			{"past last page", ListFilter{Page: 3, PageSize: 2, Keyword: "needle"}, []uint64{}, 3},
		}
		for _, keyword := range []string{"%", "_", `\`} {
			cases = append(cases, struct {
				name   string
				filter ListFilter
				ids    []uint64
				total  int64
			}{"literal " + keyword, ListFilter{Page: 1, PageSize: 10, Keyword: keyword}, []uint64{2, 1}, 2})
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				list, err := svc.List(ctx, tc.filter)
				if err != nil {
					t.Fatal(err)
				}
				ids := make([]uint64, 0, len(list.Items))
				for _, item := range list.Items {
					ids = append(ids, item.ID)
				}
				assertIDs(t, list.Total, ids, tc.ids, tc.total)
			})
		}
	})

	t.Run("persons", func(t *testing.T) {
		svc, err := NewPersonService(db)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name   string
			filter ListFilter
			ids    []uint64
			total  int64
		}{
			{"filtered second page", ListFilter{Page: 2, PageSize: 2, Keyword: "needle", Role: "leader"}, []uint64{1}, 3},
			{"exact open id", ListFilter{Page: 1, PageSize: 10, OpenID: `ou_%\`}, []uint64{1}, 1},
			{"partial open id", ListFilter{Page: 1, PageSize: 10, OpenID: "ou_"}, []uint64{}, 0},
			{"exact role", ListFilter{Page: 1, PageSize: 10, Role: "lead"}, []uint64{}, 0},
			{"combined filters", ListFilter{Page: 1, PageSize: 10, OpenID: `ou_%\`, Role: "colleague"}, []uint64{}, 0},
			{"empty", ListFilter{Page: 1, PageSize: 10, Keyword: "missing"}, []uint64{}, 0},
		}
		for _, keyword := range []string{"PersonName", "EnglishName", "Architect", `needle %_\`, "ou_third", "colleague"} {
			want := uint64(1)
			if keyword == "ou_third" {
				want = 3
			}
			if keyword == "colleague" {
				want = 4
			}
			cases = append(cases, struct {
				name   string
				filter ListFilter
				ids    []uint64
				total  int64
			}{"keyword " + keyword, ListFilter{Page: 1, PageSize: 10, Keyword: keyword}, []uint64{want}, 1})
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				list, err := svc.List(ctx, tc.filter)
				if err != nil {
					t.Fatal(err)
				}
				ids := make([]uint64, 0, len(list.Items))
				for _, item := range list.Items {
					ids = append(ids, item.ID)
				}
				assertIDs(t, list.Total, ids, tc.ids, tc.total)
			})
		}
	})

	t.Run("key matters", func(t *testing.T) {
		svc, err := NewKeyMatterService(db)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name, keyword string
			page, size    int
			closed        bool
			ids           []uint64
			total         int64
		}{
			{"summary second page", "needle", 2, 2, false, []uint64{1}, 3},
			{"include closed", "needle", 2, 2, true, []uint64{2, 1}, 4},
			{"title", "MatterTitle", 1, 10, false, []uint64{1}, 1},
			{"status", "推进中", 1, 10, false, []uint64{1}, 1},
			{"literal special characters", `%_\`, 1, 10, false, []uint64{1}, 1},
			{"empty", "missing", 1, 10, true, []uint64{}, 0},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				list, err := svc.List(ctx, KeyMatterFilter{ListFilter: ListFilter{Page: tc.page, PageSize: tc.size, Keyword: tc.keyword}, IncludeClosed: tc.closed})
				if err != nil {
					t.Fatal(err)
				}
				ids := make([]uint64, 0, len(list.Items))
				for _, item := range list.Items {
					ids = append(ids, item.ID)
				}
				assertIDs(t, list.Total, ids, tc.ids, tc.total)
			})
		}
	})

	t.Run("resources", func(t *testing.T) {
		svc, err := NewResourceService(db)
		if err != nil {
			t.Fatal(err)
		}
		base := ListFilter{Page: 1, PageSize: 10}
		cases := []struct {
			name   string
			filter ResourceFilter
			ids    []uint64
			total  int64
		}{
			{"combined second page", ResourceFilter{ListFilter: ListFilter{Page: 2, PageSize: 2, Keyword: "needle"}, PersonOpenID: `ou_%\`, PersonID: id(1), ProjectID: id(1), PrincipalOnly: true, ActiveOnly: true}, []uint64{1}, 3},
			{"exact person open id", ResourceFilter{ListFilter: base, PersonOpenID: `ou_%\`}, []uint64{5, 3, 2, 1}, 4},
			{"partial person open id", ResourceFilter{ListFilter: base, PersonOpenID: "ou_"}, []uint64{}, 0},
			{"missing person", ResourceFilter{ListFilter: base, PersonOpenID: "ou_missing"}, []uint64{}, 0},
			{"missing person id", ResourceFilter{ListFilter: base, PersonID: id(999)}, []uint64{}, 0},
			{"contradictory person filters", ResourceFilter{ListFilter: base, PersonOpenID: `ou_%\`, PersonID: id(2)}, []uint64{}, 0},
			{"principal", ResourceFilter{ListFilter: base, PrincipalOnly: true}, []uint64{5, 3, 2, 1}, 4},
			{"empty", ResourceFilter{ListFilter: ListFilter{Page: 1, PageSize: 10, Keyword: "missing"}}, []uint64{}, 0},
		}
		for _, keyword := range []string{"ResourceTitle", "repo", "example.test", "/workspace/source", `%_\`, "PersonName", "ProjectName"} {
			want := []uint64{1}
			if keyword == "PersonName" || keyword == "ProjectName" {
				want = []uint64{5, 3, 2, 1}
			}
			cases = append(cases, struct {
				name   string
				filter ResourceFilter
				ids    []uint64
				total  int64
			}{"keyword " + keyword, ResourceFilter{ListFilter: ListFilter{Page: 1, PageSize: 10, Keyword: keyword}}, want, int64(len(want))})
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				list, err := svc.List(ctx, tc.filter)
				if err != nil {
					t.Fatal(err)
				}
				ids := make([]uint64, 0, len(list.Items))
				for _, item := range list.Items {
					ids = append(ids, item.ID)
				}
				assertIDs(t, list.Total, ids, tc.ids, tc.total)
				if list.ActiveTotal != 4 {
					t.Fatalf("active_total = %d, want global count 4", list.ActiveTotal)
				}
			})
		}
	})

	t.Run("groups", func(t *testing.T) {
		svc, err := NewGroupBackgroundService(db, nil)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name, chatID, keyword string
			related               bool
			ids                   []uint64
			broadened             bool
		}{
			{"exact chat id", `oc_%\`, "", false, []uint64{1}, false},
			{"partial chat id", "oc_", "", false, []uint64{}, false},
			{"missing chat", "oc_missing", "", false, []uint64{}, false},
			{"related remains constrained", `oc_%\`, "", true, []uint64{}, false},
			{"keyword still broadens", `oc_%\`, "needle", true, []uint64{1}, true},
			{"legacy wildcard keyword", "", "%", false, []uint64{2, 1}, false},
			{"keyword and id intersect", `oc_%\`, "missing", false, []uint64{}, false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				list, err := svc.List(ctx, GroupFilter{ListFilter: ListFilter{Page: 1, PageSize: 10}, ChatID: tc.chatID, Keyword: tc.keyword, RelatedOnly: tc.related})
				if err != nil {
					t.Fatal(err)
				}
				ids := make([]uint64, 0, len(list.Items))
				for _, item := range list.Items {
					ids = append(ids, item.ID)
				}
				assertIDs(t, list.Total, ids, tc.ids, int64(len(tc.ids)))
				if list.Broadened != tc.broadened {
					t.Fatalf("broadened = %v, want %v", list.Broadened, tc.broadened)
				}
			})
		}
	})
}
