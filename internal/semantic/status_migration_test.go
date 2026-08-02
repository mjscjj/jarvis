package semantic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

func TestMigrateLegacyTodoStatusesMigratesAllPointsAndIsIdempotent(t *testing.T) {
	client := &fakeLegacyStatusClient{statuses: []string{
		"auto", "auto", "dropped", "extracted", "materialized", "observing",
	}}

	for run := 1; run <= 2; run++ {
		if err := migrateLegacyTodoStatuses(context.Background(), client, "todo_semantic"); err != nil {
			t.Fatalf("migrateLegacyTodoStatuses() run %d: %v", run, err)
		}
	}

	want := []string{"materialized", "materialized", "observing", "extracted", "materialized", "observing"}
	if len(client.statuses) != len(want) {
		t.Fatalf("statuses length = %d, want %d", len(client.statuses), len(want))
	}
	for position := range want {
		if client.statuses[position] != want[position] {
			t.Errorf("status[%d] = %q, want %q", position, client.statuses[position], want[position])
		}
	}
	if client.setCalls != 2 {
		t.Fatalf("SetPayload calls = %d, want 2 (second migration must be a no-op)", client.setCalls)
	}
}

func TestMigrateLegacyTodoStatusesFailsBeforeNextMapping(t *testing.T) {
	client := &fakeLegacyStatusClient{
		statuses: []string{"auto", "dropped"},
		setErr:   errors.New("qdrant unavailable"),
	}

	err := migrateLegacyTodoStatuses(context.Background(), client, "todo_semantic")
	if err == nil || !strings.Contains(err.Error(), `"auto" to "materialized"`) || !strings.Contains(err.Error(), "qdrant unavailable") {
		t.Fatalf("migrateLegacyTodoStatuses() error = %v, want auto migration failure", err)
	}
	if client.setCalls != 1 {
		t.Fatalf("SetPayload calls = %d, want 1", client.setCalls)
	}
	if client.statuses[0] != "auto" || client.statuses[1] != "dropped" {
		t.Fatalf("statuses changed after failed first write: %v", client.statuses)
	}
}

func TestMigrateLegacyTodoStatusesRejectsIncompleteUpdate(t *testing.T) {
	client := &fakeLegacyStatusClient{
		statuses:     []string{"auto"},
		updateStatus: qdrant.UpdateStatus_Acknowledged,
	}

	err := migrateLegacyTodoStatuses(context.Background(), client, "todo_semantic")
	if err == nil || !strings.Contains(err.Error(), "want Completed") {
		t.Fatalf("migrateLegacyTodoStatuses() error = %v, want incomplete update rejection", err)
	}
}

func TestMigrateLegacyTodoStatusesRejectsNilUpdateResult(t *testing.T) {
	client := &fakeLegacyStatusClient{
		statuses:  []string{"auto"},
		nilResult: true,
	}

	err := migrateLegacyTodoStatuses(context.Background(), client, "todo_semantic")
	if err == nil || !strings.Contains(err.Error(), "update status=UnknownUpdateStatus, want Completed") {
		t.Fatalf("migrateLegacyTodoStatuses() error = %v, want nil update result rejection", err)
	}
}

type fakeLegacyStatusClient struct {
	statuses     []string
	setErr       error
	setCalls     int
	updateStatus qdrant.UpdateStatus
	nilResult    bool
}

func (c *fakeLegacyStatusClient) Count(_ context.Context, request *qdrant.CountPoints) (uint64, error) {
	if request.GetCollectionName() != "todo_semantic" {
		return 0, errors.New("unexpected collection")
	}
	if !request.GetExact() {
		return 0, errors.New("count must be exact")
	}
	status, err := statusFromFilter(request.GetFilter())
	if err != nil {
		return 0, err
	}
	var count uint64
	for _, current := range c.statuses {
		if current == status {
			count++
		}
	}
	return count, nil
}

func (c *fakeLegacyStatusClient) SetPayload(_ context.Context, request *qdrant.SetPayloadPoints) (*qdrant.UpdateResult, error) {
	c.setCalls++
	if c.setErr != nil {
		return nil, c.setErr
	}
	if c.nilResult {
		return nil, nil
	}
	if request.GetCollectionName() != "todo_semantic" || !request.GetWait() {
		return nil, errors.New("unexpected SetPayload options")
	}
	from, err := statusFromFilter(request.GetPointsSelector().GetFilter())
	if err != nil {
		return nil, err
	}
	to := request.GetPayload()["status"].GetStringValue()
	if to == "" {
		return nil, errors.New("empty target status")
	}
	status := c.updateStatus
	if status == qdrant.UpdateStatus_UnknownUpdateStatus {
		status = qdrant.UpdateStatus_Completed
	}
	if status == qdrant.UpdateStatus_Completed {
		for position, current := range c.statuses {
			if current == from {
				c.statuses[position] = to
			}
		}
	}
	return &qdrant.UpdateResult{Status: status}, nil
}

func statusFromFilter(filter *qdrant.Filter) (string, error) {
	if filter == nil || len(filter.GetMust()) != 1 {
		return "", errors.New("expected one status filter")
	}
	field := filter.GetMust()[0].GetField()
	if field == nil || field.GetKey() != "status" || field.GetMatch() == nil || field.GetMatch().GetKeyword() == "" {
		return "", errors.New("invalid status filter")
	}
	return field.GetMatch().GetKeyword(), nil
}
