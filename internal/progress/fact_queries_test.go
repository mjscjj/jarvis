package progress

import (
	"context"
	"testing"
	"time"

	"jarvis/internal/domain"
)

func TestFactTimelineSeparatesTodayFromEarlierSubjectDays(t *testing.T) {
	service := newFactTestService(t)
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	service.now = func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, location) }
	yesterday := time.Date(2026, 8, 7, 0, 0, 0, 0, location)
	today := yesterday.AddDate(0, 0, 1)
	rows := []domain.Fact{
		{SubjectType: "topic", SubjectID: 1, Description: "today detail", OccurredAt: today.Add(9 * time.Hour), CreatedAt: today.Add(9 * time.Hour)},
		{SubjectType: "topic", SubjectID: 1, Description: "yesterday detail", OccurredAt: yesterday.Add(9 * time.Hour), CreatedAt: yesterday.Add(10 * time.Hour)},
		{SubjectType: "topic", SubjectID: 1, Description: "older yesterday detail", OccurredAt: yesterday.Add(8 * time.Hour), CreatedAt: yesterday.Add(8 * time.Hour)},
		{SubjectType: "topic", SubjectID: 2, Description: "other subject", OccurredAt: yesterday.Add(10 * time.Hour), CreatedAt: today.Add(time.Hour)},
	}
	if err := service.db.Create(&rows).Error; err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	result, err := service.FactTimeline(context.Background(), FactTimelineFilter{Days: 3, Location: location})
	if err != nil {
		t.Fatalf("FactTimeline: %v", err)
	}
	if len(result.Days) != 3 || len(result.Days[0].Details) != 1 || result.Days[0].Details[0].Description != "today detail" {
		t.Fatalf("today = %#v", result.Days[0])
	}
	if result.Days[1].DetailCount != 3 || len(result.Days[1].Subjects) != 2 {
		t.Fatalf("yesterday = %#v", result.Days[1])
	}
	states := map[uint64]FactSubjectDayView{}
	for _, subject := range result.Days[1].Subjects {
		states[subject.SubjectID] = subject
	}
	if states[1].DetailCount != 2 || !states[1].LatestOccurredAt.Equal(yesterday.Add(9*time.Hour)) {
		t.Fatalf("subject 1 = %#v, want two details latest 09:00", states[1])
	}
	if states[2].DetailCount != 1 {
		t.Fatalf("subject 2 = %#v, want one detail", states[2])
	}
}

func TestSearchFactsFindsHiddenDetailAndPaginates(t *testing.T) {
	service := newFactTestService(t)
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	insertFact(t, service, "topic", 7, "first hidden detail", now, nil)
	insertFact(t, service, "topic", 7, "second hidden detail", now.Add(time.Minute), nil)
	insertFact(t, service, "topic", 8, "unrelated", now.Add(2*time.Minute), nil)

	result, err := service.SearchFacts(context.Background(), FactSearchFilter{Query: "hidden", Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if result.Total != 2 || len(result.Items) != 1 || result.Items[0].Description != "first hidden detail" {
		t.Fatalf("result = %#v", result)
	}

	bySubject, err := service.SearchFacts(context.Background(), FactSearchFilter{Query: "topic/8", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("SearchFacts subject fallback: %v", err)
	}
	if bySubject.Total != 1 || bySubject.Items[0].Description != "unrelated" {
		t.Fatalf("subject result = %#v", bySubject)
	}
}

func TestSearchFactsWithoutKeywordPaginatesInStorageOrder(t *testing.T) {
	service := newFactTestService(t)
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	insertFact(t, service, "topic", 7, "oldest", now, nil)
	insertFact(t, service, "topic", 7, "middle", now.Add(time.Minute), nil)
	insertFact(t, service, "topic", 7, "newest", now.Add(2*time.Minute), nil)

	result, err := service.SearchFacts(context.Background(), FactSearchFilter{Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if result.Total != 3 || len(result.Items) != 1 || result.Items[0].Description != "middle" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFactSubjectLabelUsesManagedResourceForCanonicalResourceType(t *testing.T) {
	service := newFactTestService(t)
	if err := service.db.AutoMigrate(&domain.ManagedResource{}); err != nil {
		t.Fatal(err)
	}
	resource := domain.ManagedResource{Title: "季度方案", ResourceType: "doc", IsActive: true}
	if err := service.db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	insertFact(t, service, "resource", resource.ID, "文档已更新", time.Now().UTC(), nil)

	result, err := service.SearchFacts(context.Background(), FactSearchFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].SubjectLabel != "季度方案" {
		t.Fatalf("result = %#v", result)
	}
}

func TestFactSubjectLabelsUseProjectRiskAndChangeTitles(t *testing.T) {
	service := newFactTestService(t)
	if err := service.db.AutoMigrate(&domain.ProjectRisk{}, &domain.ProjectChange{}); err != nil {
		t.Fatal(err)
	}
	risk := domain.ProjectRisk{ProjectID: 1, Title: "Capacity shortage"}
	change := domain.ProjectChange{ProjectID: 1, Title: "Narrow scope", ChangedAt: time.Now().UTC()}
	if err := service.db.Create(&risk).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.db.Create(&change).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	insertFact(t, service, "project_risk", risk.ID, "risk fact", now, nil)
	insertFact(t, service, "project_change", change.ID, "change fact", now.Add(time.Minute), nil)

	result, err := service.SearchFacts(context.Background(), FactSearchFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, item := range result.Items {
		labels[item.SubjectType] = item.SubjectLabel
	}
	if labels["project_risk"] != risk.Title || labels["project_change"] != change.Title {
		t.Fatalf("labels = %#v", labels)
	}
}

func TestCanonicalResourceQueryIncludesLegacyFacts(t *testing.T) {
	service := newFactTestService(t)
	if err := service.db.AutoMigrate(&domain.ManagedResource{}); err != nil {
		t.Fatal(err)
	}
	resource := domain.ManagedResource{Title: "历史方案", ResourceType: "doc", IsActive: true}
	if err := service.db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	insertFact(t, service, "managed_resource", resource.ID, "旧类型事实", now, nil)
	insertFact(t, service, "resource", resource.ID, "新类型事实", now.Add(time.Minute), nil)

	result, err := service.SearchFacts(context.Background(), FactSearchFilter{SubjectType: "resource", SubjectID: resource.ID, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || len(result.Items) != 2 {
		t.Fatalf("result = %#v", result)
	}
	for _, item := range result.Items {
		if item.SubjectType != "resource" || item.SubjectLabel != "历史方案" {
			t.Fatalf("item = %#v", item)
		}
	}
}
