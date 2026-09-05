package execute

import (
	"context"
	"fmt"
	"jarvis/internal/domain"
)

// Only the availability and latest outcome are injected. Full attempts,
// including effects on failed attempts, are read explicitly via run tools.
type runHistory struct {
	Count        int64  `json:"count"`
	LatestID     uint64 `json:"latest_id,omitempty"`
	LatestStatus string `json:"latest_status,omitempty"`
	Read         string `json:"read"`
}

func (e *AgentExecutor) loadRunHistory(ctx context.Context, taskID, currentRunID uint64) (*runHistory, error) {
	h := &runHistory{Read: fmt.Sprintf("list-task-runs --id %d; get-task-run --id RUN_ID", taskID)}
	q := e.store.db.WithContext(ctx).Model(&domain.ExecutionRun{}).Where("task_id = ? AND id <> ?", taskID, currentRunID)
	if err := q.Count(&h.Count).Error; err != nil {
		return nil, err
	}
	if h.Count > 0 {
		var row domain.ExecutionRun
		if err := q.Select("id,status").Order("id DESC").First(&row).Error; err != nil {
			return nil, err
		}
		h.LatestID = row.ID
		h.LatestStatus = row.Status
	}
	return h, nil
}
