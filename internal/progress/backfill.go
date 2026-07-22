package progress

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

type BackfillStats struct {
	TasksScanned  int `json:"tasks_scanned"`
	EventsCreated int `json:"events_created"`
}

// BackfillTaskSnapshots creates one snapshot_imported event only for Tasks that
// have no event history. It does not infer past transitions or reuse updated_at.
// The function is intentionally one-shot and idempotent, not part of startup.
func BackfillTaskSnapshots(ctx context.Context, db *gorm.DB, cutoverAt time.Time) (*BackfillStats, error) {
	if db == nil {
		return nil, fmt.Errorf("backfill task snapshots db is nil")
	}
	if cutoverAt.IsZero() {
		return nil, fmt.Errorf("%w: cutover_at is required", ErrInvalidInput)
	}
	var tasks []domain.Task
	if err := db.WithContext(ctx).Table("task").Select("task.*").
		Joins("LEFT JOIN task_event ON task_event.task_id = task.id").
		Where("task_event.id IS NULL").Order("task.id ASC").Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("list Tasks without progress events: %w", err)
	}
	stats := &BackfillStats{TasksScanned: len(tasks)}
	for i := range tasks {
		task := &tasks[i]
		if err := AppendTaskEvent(db.WithContext(ctx), TaskEventInput{
			TaskID: task.ID, TaskVersion: task.Version, EventType: "snapshot_imported",
			ToStatus: task.Status, ActorType: "migration",
			Detail: map[string]any{"reason": "progress_event_cutover"}, OccurredAt: cutoverAt.UTC(),
		}); err != nil {
			return stats, err
		}
		stats.EventsCreated++
	}
	return stats, nil
}
