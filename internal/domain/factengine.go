package domain

import (
	"time"
)

// FactSourceCursor is the high-water mark of one offline fact-extraction source.
// The offline engine reads material whose database id is above LastID, so a run
// that dies halfway replays from the last committed watermark instead of
// rescanning the whole table or silently skipping a batch.
//
// Source is a free string ("message", "todo" and "task" today). One row per
// source keeps the sources independent: adding one is
// an insert, and one source falling behind never holds the others back.
type FactSourceCursor struct {
	Source string `gorm:"column:source;primaryKey"`

	LastID uint64 `gorm:"column:last_id;not null;default:0"`

	// LastOccurredAt is when the newest consumed material happened. It is
	// diagnostic only — LastID alone drives the scan — and answers "how far
	// behind is this source right now" without a join.
	LastOccurredAt *time.Time `gorm:"column:last_occurred_at"`

	UpdatedAt time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (FactSourceCursor) TableName() string { return "fact_source_cursor" }

// FactEngineModels returns the offline fact engine's own tables. The facts it
// produces live in the shared fact table (see ProgressModels).
func FactEngineModels() []any { return []any{&FactSourceCursor{}} }
