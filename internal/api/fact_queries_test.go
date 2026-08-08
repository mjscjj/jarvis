package api

import (
	"bytes"
	"context"
	"testing"
	"time"

	"jarvis/internal/factengine"
	"jarvis/internal/progress"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeFactQueries struct {
	timeline progress.FactTimelineFilter
	search   progress.FactSearchFilter
}

func (f *fakeFactQueries) FactTimeline(_ context.Context, filter progress.FactTimelineFilter) (progress.FactTimelineView, error) {
	f.timeline = filter
	return progress.FactTimelineView{Timezone: filter.Location.String()}, nil
}

func (f *fakeFactQueries) SearchFacts(_ context.Context, filter progress.FactSearchFilter) (progress.FactSearchView, error) {
	f.search = filter
	return progress.FactSearchView{Page: filter.Page, PageSize: filter.PageSize}, nil
}

func TestFactTimelineParsesSubjectAndDays(t *testing.T) {
	service := &fakeFactQueries{}
	h := server.New()
	h.GET("/api/facts/timeline", FactTimeline(service, time.FixedZone("Asia/Shanghai", 8*60*60)))
	response := ut.PerformRequest(h.Engine, "GET", "/api/facts/timeline?days=5&subject_type=project&subject_id=9", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if service.timeline.Days != 5 || service.timeline.SubjectType != "project" || service.timeline.SubjectID != 9 {
		t.Fatalf("filter = %#v", service.timeline)
	}
}

func TestSearchFactsParsesFilters(t *testing.T) {
	service := &fakeFactQueries{}
	h := server.New()
	h.GET("/api/facts/search", SearchFacts(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/facts/search?q=launch&layer=detail&subject_type=task&subject_id=7&page=2&page_size=20&from=2026-08-01T00:00:00Z", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if service.search.Query != "launch" || service.search.Layer != "detail" || service.search.SubjectID != 7 || service.search.Page != 2 || service.search.PageSize != 20 || service.search.From == nil {
		t.Fatalf("filter = %#v", service.search)
	}
}

type fakeRollupGenerator struct {
	allDayCalls  int
	subjectCalls int
	subjectType  string
	subjectID    uint64
}

func (f *fakeRollupGenerator) RollupDay(context.Context, time.Time) (factengine.RollupStats, error) {
	f.allDayCalls++
	return factengine.RollupStats{Subjects: 2}, nil
}

func (f *fakeRollupGenerator) RollupSubjectDay(_ context.Context, _ time.Time, subjectType string, subjectID uint64) (factengine.RollupStats, error) {
	f.subjectCalls++
	f.subjectType = subjectType
	f.subjectID = subjectID
	return factengine.RollupStats{Subjects: 1}, nil
}

func TestGenerateFactRollupsTargetsOneSubject(t *testing.T) {
	generator := &fakeRollupGenerator{}
	h := server.New()
	h.POST("/api/fact-rollups/generate", GenerateFactRollups(generator, time.UTC))
	body := []byte(`{"date":"2026-08-07","subject_type":"project","subject_id":9}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/fact-rollups/generate", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if generator.allDayCalls != 0 || generator.subjectCalls != 1 || generator.subjectType != "project" || generator.subjectID != 9 {
		t.Fatalf("generator = %#v", generator)
	}
}

func TestGenerateFactRollupsRejectsPartialSubject(t *testing.T) {
	h := server.New()
	h.POST("/api/fact-rollups/generate", GenerateFactRollups(&fakeRollupGenerator{}, time.UTC))
	body := []byte(`{"date":"2026-08-07","subject_type":"project"}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/fact-rollups/generate", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
}
