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
