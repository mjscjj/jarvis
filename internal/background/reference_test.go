package background

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"

	"gorm.io/gorm"
)

func TestParseReferences(t *testing.T) {
	t.Parallel()
	content := `[唐建科](person:12) 影响 [Agent Runtime](project:45)。根因见 [Task 393](task:393)。忽略 [普通链接](https://example.com)。`
	refs := ParseReferences(content)
	if len(refs) != 3 {
		t.Fatalf("refs = %#v, want 3", refs)
	}
	if refs[0] != (Reference{Type: "person", ID: 12}) || refs[1] != (Reference{Type: "project", ID: 45}) || refs[2] != (Reference{Type: "task", ID: 393}) {
		t.Fatalf("refs = %#v", refs)
	}
	if got := ParseReferences(`[重复](person:12) 再提 [同一人](person:12)`); len(got) != 1 || got[0].ID != 12 {
		t.Fatalf("dedup = %#v", got)
	}
}

// The maintenance prompt is the only place the reference write form is stated,
// and ParseReferences is its only reader. An example the parser cannot read
// leaves every page link invisible without any error, so assert the round trip
// against the prompt file itself instead of against a copy of its wording.
func TestEntityPageGuidanceExampleParsesAsAReference(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", "conf", "prompts", "entity-page-guidance.md"))
	if err != nil {
		t.Fatalf("read FactEngine system prompt: %v", err)
	}
	if refs := ParseReferences(string(raw)); len(refs) == 0 {
		t.Fatal("entity page guidance shows no reference example ParseReferences accepts")
	}
}

func TestValidateReferencesFailFast(t *testing.T) {
	db := openBackgroundTestDB(t)
	person := domain.Person{OpenID: "ou_ref", Name: "存在的人", Role: "key", PriorityWeight: 0.8, IsActive: true}
	if err := db.Create(&person).Error; err != nil {
		t.Fatalf("create person: %v", err)
	}
	if err := ValidateReferences(t.Context(), db, []Reference{{Type: "person", ID: person.ID}}); err != nil {
		t.Fatalf("existing person rejected: %v", err)
	}
	err := ValidateReferences(t.Context(), db, []Reference{{Type: "person", ID: 9999}})
	if err == nil {
		t.Fatal("missing person accepted")
	}
	if !strings.Contains(err.Error(), "referenced person:9999 does not exist") {
		t.Fatalf("error = %q, want missing-target fail-fast", err)
	}
}

func TestFindBacklinksHitsWorldEntityTables(t *testing.T) {
	db := openBackgroundTestDB(t)
	token := "[某人](person:12)"
	summary := token + " 被引用"
	project := domain.Project{Name: "P", Role: "owner", Status: "active", Priority: 1, Summary: &summary}
	person := domain.Person{OpenID: "ou_a", Name: "A", Role: "leader", PriorityWeight: 1, IsActive: true, Summary: &summary}
	matter := domain.KeyMatter{Title: "M", Status: "open", Summary: &summary}
	groupName := "G"
	group := domain.Group{ChatID: "oc_back", ChatMode: "group", Name: &groupName, Tier: "hot", RelatedGroup: true, Summary: &summary}
	resource := domain.ManagedResource{Title: "R", ResourceType: "doc", IsActive: true, Summary: &summary}
	principal := domain.PrincipalProfile{OpenID: "ou_me", Name: "我", Summary: &summary}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.Create(&person).Error; err != nil {
		t.Fatalf("create person: %v", err)
	}
	if err := db.Create(&matter).Error; err != nil {
		t.Fatalf("create key matter: %v", err)
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatalf("create resource: %v", err)
	}
	if err := db.Create(&principal).Error; err != nil {
		t.Fatalf("create principal: %v", err)
	}
	unrelated := "没有引用"
	if err := db.Create(&domain.Project{Name: "Other", Role: "owner", Status: "active", Priority: 2, Summary: &unrelated}).Error; err != nil {
		t.Fatalf("create unrelated: %v", err)
	}
	links, err := FindBacklinks(t.Context(), db, "person", 12)
	if err != nil {
		t.Fatalf("FindBacklinks() error = %v", err)
	}
	got := map[string]bool{}
	for _, link := range links {
		got[link.Type] = true
	}
	for _, want := range []string{"project", "person", "key_matter", "group", "resource", "principal"} {
		if !got[want] {
			t.Fatalf("backlinks missing %s: %#v", want, links)
		}
	}
	if len(links) != 6 {
		t.Fatalf("backlinks = %#v, want 6", links)
	}
}

func openBackgroundTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return db
}
