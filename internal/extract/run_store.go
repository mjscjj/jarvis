package extract

import (
	"context"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"
)

func (s *PipelineStore) StartExtractionRun(ctx context.Context, chatID string, startedAt time.Time) (uint64, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return 0, fmt.Errorf("start extraction run: chat_id is empty")
	}
	if startedAt.IsZero() {
		return 0, fmt.Errorf("start extraction run chat_id=%s: started_at is zero", chatID)
	}
	run := &domain.ExtractionRun{ChatID: chatID, Status: "running", StartedAt: startedAt.UTC()}
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return 0, fmt.Errorf("start extraction run chat_id=%s: %w", chatID, err)
	}
	return run.ID, nil
}

func (s *PipelineStore) FinishExtractionRun(ctx context.Context, runID uint64, finish ExtractionRunFinish) error {
	if runID == 0 {
		return fmt.Errorf("finish extraction run: run_id must be positive")
	}
	if finish.Status != "succeeded" && finish.Status != "failed" {
		return fmt.Errorf("finish extraction run id=%d: invalid status %q", runID, finish.Status)
	}
	if finish.MessageCount < 0 || finish.TodoCount < 0 {
		return fmt.Errorf("finish extraction run id=%d: counts must not be negative", runID)
	}
	if finish.FinishedAt.IsZero() {
		return fmt.Errorf("finish extraction run id=%d: finished_at is zero", runID)
	}
	if err := finish.Usage.Validate(); err != nil {
		return fmt.Errorf("finish extraction run id=%d: %w", runID, err)
	}

	var run domain.ExtractionRun
	found := s.db.WithContext(ctx).Where("id = ?", runID).Limit(1).Find(&run)
	if found.Error != nil {
		return fmt.Errorf("load extraction run id=%d: %w", runID, found.Error)
	}
	if found.RowsAffected != 1 {
		return fmt.Errorf("finish extraction run id=%d: not found", runID)
	}
	if run.Status != "running" {
		return fmt.Errorf("finish extraction run id=%d: status=%s, want running", runID, run.Status)
	}
	finishedAt := finish.FinishedAt.UTC()
	durationMs := finishedAt.Sub(run.StartedAt).Milliseconds()
	if durationMs < 0 {
		return fmt.Errorf("finish extraction run id=%d: finished_at precedes started_at", runID)
	}
	updates := map[string]any{
		"status": finish.Status, "message_count": finish.MessageCount, "todo_count": finish.TodoCount,
		"error_detail": finish.ErrorDetail, "finished_at": finishedAt, "duration_ms": durationMs,
	}
	if finish.Usage.Reported {
		updates["input_tokens"] = finish.Usage.InputTokens
		updates["cached_input_tokens"] = finish.Usage.CachedInputTokens
		updates["output_tokens"] = finish.Usage.OutputTokens
		updates["reasoning_output_tokens"] = finish.Usage.ReasoningOutputTokens
	}
	updated := s.db.WithContext(ctx).Model(&domain.ExtractionRun{}).
		Where("id = ? AND status = ?", runID, "running").Updates(updates)
	if updated.Error != nil {
		return fmt.Errorf("finish extraction run id=%d: %w", runID, updated.Error)
	}
	if updated.RowsAffected != 1 {
		return fmt.Errorf("finish extraction run id=%d: concurrent status change", runID)
	}
	return nil
}
