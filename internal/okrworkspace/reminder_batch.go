package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"
)

const (
	reminderBatchRunning   = "running"
	reminderBatchSucceeded = "succeeded"
	reminderBatchFailed    = "failed"
)

type ReminderBatchView struct {
	ID             string              `json:"id"`
	Quarter        string              `json:"quarter"`
	Week           string              `json:"week"`
	Trigger        string              `json:"trigger"`
	Status         string              `json:"status"`
	SendEnabled    bool                `json:"send_enabled"`
	RecipientCount int                 `json:"recipient_count"`
	MissingCount   int                 `json:"missing_count"`
	Summary        ReminderSummary     `json:"summary"`
	Recipients     []ReminderRecipient `json:"recipients"`
	LastError      string              `json:"last_error,omitempty"`
	StartedAt      string              `json:"started_at"`
	FinishedAt     string              `json:"finished_at,omitempty"`
}

type ReminderBatchList struct {
	Mode        string              `json:"mode"`
	SendEnabled bool                `json:"send_enabled"`
	Batches     []ReminderBatchView `json:"batches"`
}

// GenerateReminderBatch persists a point-in-time preview for human review. It
// never invokes lark-cli or another external writer.
func (s *Service) GenerateReminderBatch(ctx context.Context, quarter, week, trigger string) (ReminderBatchView, error) {
	quarter = strings.TrimSpace(quarter)
	trigger = strings.TrimSpace(trigger)
	if quarter == "" {
		return ReminderBatchView{}, fmt.Errorf("quarter is required")
	}
	if !weekPattern.MatchString(week) {
		return ReminderBatchView{}, fmt.Errorf("week must use YYYY-Www")
	}
	if trigger != "manual" && trigger != "scheduler" {
		return ReminderBatchView{}, fmt.Errorf("trigger must be manual or scheduler")
	}

	started := time.Now().UTC()
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", quarter, week, trigger, started.UnixNano())))
	record := domain.ReminderBatch{
		ID: "reminder-" + hex.EncodeToString(digest[:8]), Quarter: quarter, Week: week,
		Trigger: trigger, Status: reminderBatchRunning, SummaryJSON: "{}", RecipientsJSON: "[]", StartedAt: started,
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return ReminderBatchView{}, fmt.Errorf("start reminder batch: %w", err)
	}

	preview, previewErr := s.ReminderPreview(ctx, quarter, week)
	finished := time.Now().UTC()
	updates := map[string]any{"finished_at": finished}
	if previewErr != nil {
		updates["status"] = reminderBatchFailed
		updates["last_error"] = previewErr.Error()
		if err := s.db.WithContext(ctx).Model(&domain.ReminderBatch{}).Where("id = ?", record.ID).Updates(updates).Error; err != nil {
			return ReminderBatchView{}, fmt.Errorf("generate reminder batch: %v; persist failure: %w", previewErr, err)
		}
		return s.reminderBatchByID(ctx, record.ID)
	}
	summaryJSON, err := json.Marshal(preview.Summary)
	if err != nil {
		return ReminderBatchView{}, fmt.Errorf("encode reminder summary: %w", err)
	}
	recipientsJSON, err := json.Marshal(preview.Recipients)
	if err != nil {
		return ReminderBatchView{}, fmt.Errorf("encode reminder recipients: %w", err)
	}
	remindableCount := 0
	for _, recipient := range preview.Recipients {
		if recipient.NeedsReminder && recipient.CanRemind {
			remindableCount++
		}
	}
	updates["status"] = reminderBatchSucceeded
	updates["recipient_count"] = remindableCount
	updates["missing_count"] = preview.Summary.MissingCount
	updates["summary_json"] = string(summaryJSON)
	updates["recipients_json"] = string(recipientsJSON)
	if err := s.db.WithContext(ctx).Model(&domain.ReminderBatch{}).Where("id = ?", record.ID).Updates(updates).Error; err != nil {
		return ReminderBatchView{}, fmt.Errorf("finish reminder batch: %w", err)
	}
	return s.reminderBatchByID(ctx, record.ID)
}

func (s *Service) ReminderBatches(ctx context.Context, quarter, week string, limit int) (ReminderBatchList, error) {
	quarter = strings.TrimSpace(quarter)
	if quarter == "" {
		return ReminderBatchList{}, fmt.Errorf("quarter is required")
	}
	if !weekPattern.MatchString(week) {
		return ReminderBatchList{}, fmt.Errorf("week must use YYYY-Www")
	}
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	var records []domain.ReminderBatch
	if err := s.db.WithContext(ctx).Where("quarter = ? AND week = ?", quarter, week).Order("started_at DESC, id DESC").Limit(limit).Find(&records).Error; err != nil {
		return ReminderBatchList{}, fmt.Errorf("list reminder batches: %w", err)
	}
	result := ReminderBatchList{Mode: "preview_only", SendEnabled: false, Batches: make([]ReminderBatchView, 0, len(records))}
	for _, record := range records {
		view, err := decodeReminderBatch(record)
		if err != nil {
			return ReminderBatchList{}, err
		}
		result.Batches = append(result.Batches, view)
	}
	return result, nil
}

func (s *Service) reminderBatchByID(ctx context.Context, id string) (ReminderBatchView, error) {
	var record domain.ReminderBatch
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		return ReminderBatchView{}, fmt.Errorf("read reminder batch: %w", err)
	}
	return decodeReminderBatch(record)
}

func decodeReminderBatch(record domain.ReminderBatch) (ReminderBatchView, error) {
	view := ReminderBatchView{
		ID: record.ID, Quarter: record.Quarter, Week: record.Week, Trigger: record.Trigger,
		Status: record.Status, SendEnabled: false, RecipientCount: record.RecipientCount,
		MissingCount: record.MissingCount, LastError: record.LastError,
		StartedAt: record.StartedAt.UTC().Format(time.RFC3339), Recipients: []ReminderRecipient{},
	}
	if record.FinishedAt != nil {
		view.FinishedAt = record.FinishedAt.UTC().Format(time.RFC3339)
	}
	if err := json.Unmarshal([]byte(record.SummaryJSON), &view.Summary); err != nil {
		return ReminderBatchView{}, fmt.Errorf("decode reminder batch %s summary: %w", record.ID, err)
	}
	if err := json.Unmarshal([]byte(record.RecipientsJSON), &view.Recipients); err != nil {
		return ReminderBatchView{}, fmt.Errorf("decode reminder batch %s recipients: %w", record.ID, err)
	}
	return view, nil
}
