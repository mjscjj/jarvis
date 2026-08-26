package background

import (
	"errors"
	"strings"
	"testing"

	"jarvis/internal/domain"
)

func TestOKRInputValidation(t *testing.T) {
	if err := (&OKRInput{Title: " ", Cycle: "2026-Q3"}).validate(); err == nil {
		t.Fatal("blank title accepted")
	}
	if err := (&OKRInput{Title: "提升区域交付", Cycle: " "}).validate(); err == nil {
		t.Fatal("blank cycle accepted")
	}
	if err := (&OKRInput{Title: "提升区域交付", Cycle: "2026-Q3", Status: "需要关注"}).validate(); err != nil {
		t.Fatalf("free-text status rejected: %v", err)
	}
}

func TestOKRHierarchyLifecycleAndFacts(t *testing.T) {
	db := openBackgroundTestDB(t)
	owner := domain.Person{OpenID: "ou_okr_owner", Name: "Emily", Role: "key", PriorityWeight: 0.9, IsActive: true}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	service, err := NewOKRService(db)
	if err != nil {
		t.Fatalf("NewOKRService() error = %v", err)
	}
	created, err := service.Create(t.Context(), OKRInput{
		Title: "公会大会顺利落地", Cycle: "2026-Q3", Status: "推进中", OwnerPersonID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Owner == nil || created.Owner.ID != owner.ID || created.Projects == nil {
		t.Fatalf("Create() = %+v, missing owner or stable projects array", created)
	}

	projects, err := NewProjectService(db)
	if err != nil {
		t.Fatalf("NewProjectService() error = %v", err)
	}
	project, err := projects.Create(t.Context(), ProjectInput{
		Name: "大会交付", Role: "owner", Status: "active", Priority: 1, OKRID: &created.ID,
	})
	if err != nil {
		t.Fatalf("create linked project: %v", err)
	}
	matters, err := NewKeyMatterService(db)
	if err != nil {
		t.Fatalf("NewKeyMatterService() error = %v", err)
	}
	if _, err := matters.Create(t.Context(), KeyMatterInput{
		Title: "完成首轮 Dry Run", Status: "推进中", ProjectID: &project.ID,
	}); err != nil {
		t.Fatalf("create linked key matter: %v", err)
	}

	hierarchy, err := service.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("Get() hierarchy error = %v", err)
	}
	if len(hierarchy.Projects) != 1 || hierarchy.Projects[0].OKRID == nil ||
		len(hierarchy.Projects[0].KeyMatters) != 1 {
		t.Fatalf("hierarchy = %+v, want OKR -> Project -> KeyMatter", hierarchy)
	}

	updated, err := service.Update(t.Context(), created.ID, OKRInput{
		Title: created.Title, Cycle: created.Cycle, Status: "有风险", OwnerPersonID: created.OwnerPersonID,
	})
	if err != nil || updated.Status != "有风险" {
		t.Fatalf("Update() = %+v, error = %v", updated, err)
	}
	if err := service.Delete(t.Context(), created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := service.Delete(t.Context(), created.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Delete() twice error = %v, want ErrInvalidInput", err)
	}

	var facts []domain.Fact
	if err := db.Where("subject_type = ? AND subject_id = ?", "okr", created.ID).Order("id ASC").Find(&facts).Error; err != nil {
		t.Fatalf("list okr facts: %v", err)
	}
	if len(facts) != 3 {
		t.Fatalf("fact count = %d, want 3: %+v", len(facts), facts)
	}
	for i, want := range []string{"创建", "状态", "已闭环"} {
		if !strings.Contains(facts[i].Description, want) {
			t.Fatalf("fact[%d] = %q, want contains %q", i, facts[i].Description, want)
		}
	}
}

func TestOKRRejectsMissingOwnerAndProjectRejectsMissingOKR(t *testing.T) {
	db := openBackgroundTestDB(t)
	okrs, _ := NewOKRService(db)
	missing := uint64(999)
	if _, err := okrs.Create(t.Context(), OKRInput{
		Title: "目标", Cycle: "2026-Q3", OwnerPersonID: &missing,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() missing owner error = %v, want ErrInvalidInput", err)
	}
	projects, _ := NewProjectService(db)
	if _, err := projects.Create(t.Context(), ProjectInput{
		Name: "项目", Role: "owner", Status: "active", Priority: 1, OKRID: &missing,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() missing okr error = %v, want ErrInvalidInput", err)
	}
}

func TestClosedOKRKeepsLinkedProjectEditableButRejectsNewLinks(t *testing.T) {
	db := openBackgroundTestDB(t)
	okrs, _ := NewOKRService(db)
	projects, _ := NewProjectService(db)

	okr, err := okrs.Create(t.Context(), OKRInput{
		Title: "季度目标", Cycle: "2026-Q3", Status: "推进中",
	})
	if err != nil {
		t.Fatalf("create OKR: %v", err)
	}
	linked, err := projects.Create(t.Context(), ProjectInput{
		Name: "既有项目", Role: "owner", Status: "active", Priority: 1, OKRID: &okr.ID,
	})
	if err != nil {
		t.Fatalf("create linked project: %v", err)
	}
	if err := okrs.Delete(t.Context(), okr.ID); err != nil {
		t.Fatalf("close OKR: %v", err)
	}

	updated, err := projects.Update(t.Context(), linked.ID, ProjectInput{
		Name: linked.Name, Role: linked.Role, Status: "done", Priority: linked.Priority, OKRID: linked.OKRID,
	})
	if err != nil {
		t.Fatalf("update project retained by closed OKR: %v", err)
	}
	if updated.Status != "done" || updated.OKRID == nil || *updated.OKRID != okr.ID {
		t.Fatalf("updated project = %+v, want retained OKR association", updated)
	}

	if _, err := projects.Create(t.Context(), ProjectInput{
		Name: "新项目", Role: "owner", Status: "active", Priority: 2, OKRID: &okr.ID,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("create project linked to closed OKR error = %v, want ErrInvalidInput", err)
	}
}
