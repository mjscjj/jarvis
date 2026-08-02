package observe

import (
	"errors"
	"testing"
	"time"

	"jarvis/internal/domain"
)

func TestNewServiceRejectsNilDB(t *testing.T) {
	t.Parallel()
	if _, err := NewService(nil); err == nil {
		t.Fatal("NewService(nil) error = nil, want failure")
	}
}

func TestValidateFilterAcceptsBothProducers(t *testing.T) {
	t.Parallel()
	for _, producer := range []string{"", domain.ObservationProducerM3, domain.ObservationProducerM5} {
		normalized, err := validateFilter(Filter{Producer: producer, Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("validateFilter(%q) error = %v", producer, err)
		}
		if normalized != producer {
			t.Fatalf("normalized = %q, want %q", normalized, producer)
		}
	}
}

func TestValidateFilterRejectsBadInput(t *testing.T) {
	t.Parallel()
	zero := uint64(0)
	tests := map[string]Filter{
		"page zero":       {Page: 0, PageSize: 20},
		"page size zero":  {Page: 1, PageSize: 0},
		"page size over":  {Page: 1, PageSize: 101},
		"unknown produce": {Producer: "m4", Page: 1, PageSize: 20},
		"project zero":    {ProjectID: &zero, Page: 1, PageSize: 20},
	}
	for name, filter := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := validateFilter(filter); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestToViewResolvesNames(t *testing.T) {
	t.Parallel()
	projectID := uint64(3)
	groupID := uint64(9)
	row := domain.Observation{
		ID: 1, Producer: domain.ObservationProducerM3,
		Subject: "Bax PC 评测范围", Content: "这轮只看看板。",
		ProjectID: &projectID, GroupID: &groupID,
		SourceMessageIDs: []byte(`["om_1","om_2"]`),
		ObservedAt:       time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
	}
	names := labels{
		projects: map[uint64]string{3: "Bax"},
		groups:   map[uint64]string{9: "Bax PC 群"},
	}
	view := toView(&row, names)
	if view.ProjectName == nil || *view.ProjectName != "Bax" {
		t.Fatalf("project_name = %v", view.ProjectName)
	}
	if view.GroupName == nil || *view.GroupName != "Bax PC 群" {
		t.Fatalf("group_name = %v", view.GroupName)
	}
	if len(view.SourceMessageIDs) != 2 {
		t.Fatalf("source_message_ids = %#v", view.SourceMessageIDs)
	}
}

// A row whose citation column cannot be parsed must still render: the
// observation text is the point, the id list is decoration.
func TestToViewTolerlatesUnreadableCitations(t *testing.T) {
	t.Parallel()
	row := domain.Observation{ID: 1, Subject: "s", Content: "c", SourceMessageIDs: []byte(`not json`)}
	view := toView(&row, labels{projects: map[uint64]string{}, groups: map[uint64]string{}})
	if view.SourceMessageIDs != nil {
		t.Fatalf("source_message_ids = %#v, want nil", view.SourceMessageIDs)
	}
	if view.Subject != "s" || view.Content != "c" {
		t.Fatalf("view = %#v", view)
	}
}
