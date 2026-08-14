package background

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

func TestUpdatePageRejectsOverLimitSummary(t *testing.T) {
	db := openBackgroundTestDB(t)
	svc := newPageService(t, db)
	project := createTestProject(t, db, "Limit", "active")
	page, err := svc.GetPage(t.Context(), PageTypeProject, project.ID)
	if err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}
	over := strings.Repeat("字", SummaryMaxChars+1)
	_, err = svc.UpdatePage(t.Context(), PageTypeProject, project.ID, UpdatePageInput{
		Content: over, IfUnchangedSince: page.UpdatedAt,
	})
	if err == nil {
		t.Fatal("over-limit summary accepted")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	n := utf8.RuneCountInString(over)
	if !strings.Contains(err.Error(), "8000") || !strings.Contains(err.Error(), "8001") {
		t.Fatalf("error = %q, want current count %d and limit %d", err, n, SummaryMaxChars)
	}
	if !strings.Contains(err.Error(), "请先压缩") {
		t.Fatalf("error = %q, want compaction guidance", err)
	}
}

func TestUpdatePageFailureDoesNotBlockOtherEntities(t *testing.T) {
	db := openBackgroundTestDB(t)
	svc := newPageService(t, db)
	failing := createTestProject(t, db, "Failing", "active")
	ok := createTestProject(t, db, "OK", "active")
	failingPage, err := svc.GetPage(t.Context(), PageTypeProject, failing.ID)
	if err != nil {
		t.Fatalf("GetPage(failing) error = %v", err)
	}
	okPage, err := svc.GetPage(t.Context(), PageTypeProject, ok.ID)
	if err != nil {
		t.Fatalf("GetPage(ok) error = %v", err)
	}
	var errs []error
	if _, err := svc.UpdatePage(t.Context(), PageTypeProject, failing.ID, UpdatePageInput{
		Content: strings.Repeat("超", SummaryMaxChars+1), IfUnchangedSince: failingPage.UpdatedAt,
	}); err != nil {
		errs = append(errs, err)
	}
	if _, err := svc.UpdatePage(t.Context(), PageTypeProject, ok.ID, UpdatePageInput{
		Content: "第二页写成功", IfUnchangedSince: okPage.UpdatedAt,
	}); err != nil {
		errs = append(errs, err)
	}
	joined := errors.Join(errs...)
	if joined == nil || len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one failure", errs)
	}
	got, err := svc.GetPage(t.Context(), PageTypeProject, ok.ID)
	if err != nil {
		t.Fatalf("GetPage(ok after) error = %v", err)
	}
	if got.Summary != "第二页写成功" {
		t.Fatalf("surviving page = %#v", got)
	}
	stale, err := svc.GetPage(t.Context(), PageTypeProject, failing.ID)
	if err != nil {
		t.Fatalf("GetPage(failing after) error = %v", err)
	}
	if stale.Summary != "" {
		t.Fatalf("failed page was written: %#v", stale)
	}
}

func TestUpdatePageWritesRevisionAndAdvancesProgressOnlyOnChange(t *testing.T) {
	db := openBackgroundTestDB(t)
	svc := newPageService(t, db)
	project := createTestProject(t, db, "Progress", "active")
	firstAt := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return firstAt }
	page, err := svc.GetPage(t.Context(), PageTypeProject, project.ID)
	if err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}
	written, err := svc.UpdatePage(t.Context(), PageTypeProject, project.ID, UpdatePageInput{
		Content: "第一版全文", IfUnchangedSince: page.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("first UpdatePage() error = %v", err)
	}
	if written.LastProgressAt == nil || !written.LastProgressAt.Equal(firstAt) {
		t.Fatalf("first last_progress_at = %v, want %s", written.LastProgressAt, firstAt)
	}
	if countPageRevisions(t, db, project.ID) != 0 {
		t.Fatal("empty-to-content write created a page_revision fact")
	}

	later := firstAt.Add(time.Hour)
	svc.now = func() time.Time { return later }
	unchanged, err := svc.UpdatePage(t.Context(), PageTypeProject, project.ID, UpdatePageInput{
		Content: "第一版全文", IfUnchangedSince: written.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("unchanged UpdatePage() error = %v", err)
	}
	if unchanged.LastProgressAt == nil || !unchanged.LastProgressAt.Equal(firstAt) {
		t.Fatalf("unchanged last_progress_at = %v, want %s", unchanged.LastProgressAt, firstAt)
	}
	if countPageRevisions(t, db, project.ID) != 0 {
		t.Fatal("unchanged write created a page_revision fact")
	}

	changedAt := later.Add(time.Hour)
	svc.now = func() time.Time { return changedAt }
	changed, err := svc.UpdatePage(t.Context(), PageTypeProject, project.ID, UpdatePageInput{
		Content: "第二版全文", IfUnchangedSince: unchanged.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("changed UpdatePage() error = %v", err)
	}
	if changed.LastProgressAt == nil || !changed.LastProgressAt.Equal(changedAt) {
		t.Fatalf("changed last_progress_at = %v, want %s", changed.LastProgressAt, changedAt)
	}
	if changed.Summary != "第二版全文" {
		t.Fatalf("changed summary = %q", changed.Summary)
	}
	var facts []domain.Fact
	if err := db.Where("subject_type = ? AND subject_id = ? AND source_kind = ?", PageTypeProject, project.ID, factSourcePageRevision).
		Find(&facts).Error; err != nil {
		t.Fatalf("list page_revision facts: %v", err)
	}
	if len(facts) != 1 || facts[0].Description != "第一版全文" {
		t.Fatalf("page_revision facts = %#v", facts)
	}
}

func TestUpdatePageConflictReturnsCurrentPage(t *testing.T) {
	db := openBackgroundTestDB(t)
	svc := newPageService(t, db)
	project := createTestProject(t, db, "CAS", "active")
	page, err := svc.GetPage(t.Context(), PageTypeProject, project.ID)
	if err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}
	if _, err := svc.UpdatePage(t.Context(), PageTypeProject, project.ID, UpdatePageInput{
		Content: "当前全文", IfUnchangedSince: page.UpdatedAt,
	}); err != nil {
		t.Fatalf("seed UpdatePage() error = %v", err)
	}
	_, err = svc.UpdatePage(t.Context(), PageTypeProject, project.ID, UpdatePageInput{
		Content: "被覆盖的草稿", IfUnchangedSince: page.UpdatedAt,
	})
	var conflict *PageConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v, want PageConflictError", err)
	}
	if conflict.Current == nil || conflict.Current.Summary != "当前全文" {
		t.Fatalf("conflict current = %#v", conflict.Current)
	}
}

func TestUpdatePageRejectsMissingReference(t *testing.T) {
	db := openBackgroundTestDB(t)
	svc := newPageService(t, db)
	project := createTestProject(t, db, "Refs", "active")
	page, err := svc.GetPage(t.Context(), PageTypeProject, project.ID)
	if err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}
	_, err = svc.UpdatePage(t.Context(), PageTypeProject, project.ID, UpdatePageInput{
		Content: "[不存在](person:9999)", IfUnchangedSince: page.UpdatedAt,
	})
	if err == nil || !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "referenced person:9999 does not exist") {
		t.Fatalf("error = %v, want missing reference", err)
	}
}

func TestListPagesDefaultActiveAndInspectionFilters(t *testing.T) {
	db := openBackgroundTestDB(t)
	svc := newPageService(t, db)
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	active := createTestProject(t, db, "ActiveProj", "active")
	archived := createTestProject(t, db, "ArchivedProj", "archived")
	leader := domain.Person{OpenID: "ou_leader", Name: "Leader", Role: "leader", PriorityWeight: 1, IsActive: true}
	colleague := domain.Person{OpenID: "ou_peer", Name: "Peer", Role: "colleague", PriorityWeight: 0.2, IsActive: true}
	if err := db.Create(&leader).Error; err != nil {
		t.Fatalf("create leader: %v", err)
	}
	if err := db.Create(&colleague).Error; err != nil {
		t.Fatalf("create colleague: %v", err)
	}
	openMatter := domain.KeyMatter{Title: "Open", Status: "open"}
	closedAt := now.Add(-time.Hour)
	closedMatter := domain.KeyMatter{Title: "Closed", Status: "done", ClosedAt: &closedAt}
	if err := db.Create(&openMatter).Error; err != nil {
		t.Fatalf("create open matter: %v", err)
	}
	if err := db.Create(&closedMatter).Error; err != nil {
		t.Fatalf("create closed matter: %v", err)
	}
	hotName, coldName := "HotGroup", "ColdGroup"
	hot := domain.Group{ChatID: "oc_hot", ChatMode: "group", Name: &hotName, Tier: "hot", RelatedGroup: true}
	cold := domain.Group{ChatID: "oc_cold", ChatMode: "group", Name: &coldName, Tier: "cold", RelatedGroup: true}
	if err := db.Create(&hot).Error; err != nil {
		t.Fatalf("create hot group: %v", err)
	}
	if err := db.Create(&cold).Error; err != nil {
		t.Fatalf("create cold group: %v", err)
	}
	activeRes := domain.ManagedResource{Title: "LiveDoc", ResourceType: "doc", IsActive: true}
	inactiveRes := domain.ManagedResource{Title: "DeadDoc", ResourceType: "doc", IsActive: false}
	if err := db.Create(&activeRes).Error; err != nil {
		t.Fatalf("create active resource: %v", err)
	}
	if err := db.Create(&inactiveRes).Error; err != nil {
		t.Fatalf("create inactive resource: %v", err)
	}
	// is_active carries a `default:1` tag, so GORM drops the false zero value on
	// insert and the row lands active. Write the column explicitly.
	if err := db.Model(&domain.ManagedResource{}).Where("id = ?", inactiveRes.ID).
		Update("is_active", false).Error; err != nil {
		t.Fatalf("deactivate resource: %v", err)
	}
	if err := db.Create(&domain.PrincipalProfile{OpenID: "ou_me", Name: "我"}).Error; err != nil {
		t.Fatalf("create principal: %v", err)
	}

	activeItems, err := svc.ListPages(t.Context(), ListPagesFilter{})
	if err != nil {
		t.Fatalf("ListPages() default error = %v", err)
	}
	if !hasPage(activeItems, PageTypeProject, active.ID) || hasPage(activeItems, PageTypeProject, archived.ID) {
		t.Fatalf("default project filter = %#v", activeItems)
	}
	if !hasPage(activeItems, PageTypePerson, leader.ID) || hasPage(activeItems, PageTypePerson, colleague.ID) {
		t.Fatalf("default person filter = %#v", activeItems)
	}
	if !hasPage(activeItems, PageTypeKeyMatter, openMatter.ID) || hasPage(activeItems, PageTypeKeyMatter, closedMatter.ID) {
		t.Fatalf("default key_matter filter = %#v", activeItems)
	}
	if !hasPage(activeItems, PageTypeGroup, hot.ID) || hasPage(activeItems, PageTypeGroup, cold.ID) {
		t.Fatalf("default group filter = %#v", activeItems)
	}
	if !hasPage(activeItems, PageTypeResource, activeRes.ID) || hasPage(activeItems, PageTypeResource, inactiveRes.ID) {
		t.Fatalf("default resource filter = %#v", activeItems)
	}
	if !hasPageType(activeItems, PageTypePrincipal) {
		t.Fatalf("default list missing principal: %#v", activeItems)
	}

	allItems, err := svc.ListPages(t.Context(), ListPagesFilter{All: true})
	if err != nil {
		t.Fatalf("ListPages(--all) error = %v", err)
	}
	if !hasPage(allItems, PageTypeProject, archived.ID) || !hasPage(allItems, PageTypePerson, colleague.ID) {
		t.Fatalf("--all missing inactive rows: %#v", allItems)
	}

	staleAt := now.Add(-10 * 24 * time.Hour)
	freshAt := now.Add(-2 * 24 * time.Hour)
	if err := db.Model(&domain.Project{}).Where("id = ?", active.ID).Update("last_progress_at", staleAt).Error; err != nil {
		t.Fatalf("set stale last_progress_at: %v", err)
	}
	if err := db.Model(&domain.Person{}).Where("id = ?", leader.ID).Update("last_progress_at", freshAt).Error; err != nil {
		t.Fatalf("set fresh last_progress_at: %v", err)
	}
	staleDays := 7
	staleItems, err := svc.ListPages(t.Context(), ListPagesFilter{StaleDays: &staleDays})
	if err != nil {
		t.Fatalf("ListPages(stale_days) error = %v", err)
	}
	if !hasPage(staleItems, PageTypeProject, active.ID) {
		t.Fatalf("stale_days missed stale project: %#v", staleItems)
	}
	if hasPage(staleItems, PageTypePerson, leader.ID) {
		t.Fatalf("stale_days included fresh person: %#v", staleItems)
	}

	over := strings.Repeat("超", SummaryMaxChars+1)
	if err := db.Model(&domain.Project{}).Where("id = ?", active.ID).Update("summary", over).Error; err != nil {
		t.Fatalf("set over-limit summary: %v", err)
	}
	overItems, err := svc.ListPages(t.Context(), ListPagesFilter{OverLimit: true})
	if err != nil {
		t.Fatalf("ListPages(over_limit) error = %v", err)
	}
	if len(overItems) != 1 || overItems[0].ID != active.ID || overItems[0].CharCount != SummaryMaxChars+1 {
		t.Fatalf("over_limit = %#v", overItems)
	}
}

func newPageService(t *testing.T, db *gorm.DB) *PageService {
	t.Helper()
	svc, err := NewPageService(db)
	if err != nil {
		t.Fatalf("NewPageService() error = %v", err)
	}
	return svc
}

func createTestProject(t *testing.T, db *gorm.DB, name, status string) domain.Project {
	t.Helper()
	project := domain.Project{Name: name, Role: "owner", Status: status, Priority: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project %s: %v", name, err)
	}
	return project
}

func countPageRevisions(t *testing.T, db *gorm.DB, projectID uint64) int {
	t.Helper()
	var count int64
	if err := db.Model(&domain.Fact{}).
		Where("subject_type = ? AND subject_id = ? AND source_kind = ?", PageTypeProject, projectID, factSourcePageRevision).
		Count(&count).Error; err != nil {
		t.Fatalf("count page_revision facts: %v", err)
	}
	return int(count)
}

func hasPage(items []PageIndexItem, pageType string, id uint64) bool {
	for _, item := range items {
		if item.Type == pageType && item.ID == id {
			return true
		}
	}
	return false
}

func hasPageType(items []PageIndexItem, pageType string) bool {
	for _, item := range items {
		if item.Type == pageType {
			return true
		}
	}
	return false
}
