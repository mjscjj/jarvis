package insight

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// DebugService backs the admin Debug panel: per-module cron run history,
// capture scan history, extraction watermarks, and recent todo/task detail.
type DebugService struct {
	db   *gorm.DB
	logs *LogReader
}

func NewDebugService(db *gorm.DB, logs *LogReader) (*DebugService, error) {
	if db == nil {
		return nil, fmt.Errorf("debug service db is nil")
	}
	if logs == nil {
		return nil, fmt.Errorf("debug service log reader is nil")
	}
	return &DebugService{db: db, logs: logs}, nil
}

// ScanRow is one capture scan_record for the debug panel.
type ScanRow struct {
	ID            uint64  `json:"id"`
	ScanType      string  `json:"scan_type"`
	ChatID        *string `json:"chat_id"`
	Status        string  `json:"status"`
	FetchedCount  int32   `json:"fetched_count"`
	InsertedCount int32   `json:"inserted_count"`
	ErrorType     *string `json:"error_type"`
	ErrorMessage  *string `json:"error_message"`
	StartedAt     string  `json:"started_at"`
	DurationMS    *int32  `json:"duration_ms"`
}

func (s *DebugService) Scans(ctx context.Context, limit int) ([]ScanRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var records []domain.ScanRecord
	if err := s.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load scan records: %w", err)
	}
	rows := make([]ScanRow, len(records))
	for i := range records {
		r := &records[i]
		rows[i] = ScanRow{
			ID: r.ID, ScanType: r.ScanType, ChatID: r.ChatID, Status: r.Status,
			FetchedCount: r.FetchedCount, InsertedCount: r.InsertedCount,
			ErrorType: r.ErrorType, ErrorMessage: r.ErrorMessage,
			StartedAt: r.StartedAt.Format(time.RFC3339), DurationMS: r.DurationMS,
		}
	}
	return rows, nil
}

// WatermarkRow is one chat's extraction cursor enriched with its name and the
// plaintext message referenced by the cursor.
type WatermarkRow struct {
	ChatID             string `json:"chat_id"`
	GroupName          string `json:"group_name"`
	LastMessageID      string `json:"last_message_id"`
	LastMessageContent string `json:"last_message_content"`
	LastScannedAt      string `json:"last_scanned_at"`
	UpdatedAt          string `json:"updated_at"`
}

func (s *DebugService) Watermarks(ctx context.Context) ([]WatermarkRow, error) {
	var marks []domain.TodoExtractWatermark
	if err := s.db.WithContext(ctx).Order("updated_at DESC").Find(&marks).Error; err != nil {
		return nil, fmt.Errorf("load extract watermarks: %w", err)
	}
	rows := make([]WatermarkRow, len(marks))
	for i := range marks {
		m := &marks[i]
		row := WatermarkRow{
			ChatID:        m.ChatID,
			LastMessageID: m.LastScannedMessageID,
			LastScannedAt: m.LastScannedAt.Format(time.RFC3339),
			UpdatedAt:     m.UpdatedAt.Format(time.RFC3339),
		}
		var message domain.Message
		if err := s.db.WithContext(ctx).
			Select("content").
			Where("chat_id = ? AND message_id = ?", m.ChatID, m.LastScannedMessageID).
			First(&message).Error; err != nil {
			return nil, fmt.Errorf("load extract watermark message chat_id=%s message_id=%s: %w", m.ChatID, m.LastScannedMessageID, err)
		}
		row.LastMessageContent = message.Content
		var group domain.Group
		if err := s.db.WithContext(ctx).Select("name").Where("chat_id = ?", m.ChatID).First(&group).Error; err == nil && group.Name != nil {
			row.GroupName = *group.Name
		}
		rows[i] = row
	}
	return rows, nil
}
