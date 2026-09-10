// Package delegation presents M3 Todos as independently tracked commitments.
// It does not create/complete Tasks or decide whether somebody has delivered.
package delegation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jarvis/internal/datatypes"
	"jarvis/internal/domain"
)

const ActionType = "delegated_followup"

var (
	ErrNotFound = errors.New("delegation not found")
	ErrConflict = errors.New("delegation version conflict; reload before updating")
	ErrInvalid  = errors.New("invalid delegation input")
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("delegation database is nil")
	}
	return &Service{db: db}, nil
}

type View struct {
	ID            uint64          `json:"id"`
	Title         string          `json:"title"` // Original title, not a mutable owner/deadline.
	SourceQuote   string          `json:"source_quote"`
	SourcePayload json.RawMessage `json:"source_payload,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	Summary       string          `json:"summary"`
	ClosedAt      *time.Time      `json:"closed_at"`
	Version       int32           `json:"version"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type Filter struct {
	State, Query string
	Page, Limit  int
}
type Page struct {
	Items []View `json:"items"`
	Total int64  `json:"total"`
}

// List starts with Todo, so an extracted commitment exists before any M5 run.
func (s *Service) List(ctx context.Context, f Filter) (*Page, error) {
	if f.Page < 1 || f.Limit < 1 || f.Limit > 100 {
		return nil, ErrInvalid
	}
	q := s.db.WithContext(ctx).Table("todo AS t").
		Joins("LEFT JOIN delegation_progress AS d ON d.todo_id = t.id").Where("t.action_type = ?", ActionType)
	switch f.State {
	case "", "open":
		q = q.Where("d.closed_at IS NULL")
	case "closed":
		q = q.Where("d.closed_at IS NOT NULL")
	case "all":
	default:
		return nil, fmt.Errorf("%w: state must be open, closed or all", ErrInvalid)
	}
	if term := strings.TrimSpace(f.Query); term != "" {
		like := "%" + term + "%"
		q = q.Where("t.title LIKE ? OR t.content LIKE ? OR d.content LIKE ?", like, like, like)
	}
	page := &Page{Items: []View{}}
	if err := q.Count(&page.Total).Error; err != nil {
		return nil, err
	}
	type row struct {
		ID                 uint64
		Title, SourceQuote string
		Content            datatypes.JSON
		ClosedAt           *time.Time
		Version            int32
		TodoUpdatedAt      time.Time
		ProgressUpdatedAt  *time.Time
	}
	var rows []row
	if err := q.Select("t.id, t.title, t.source_quote, d.content, d.closed_at, COALESCE(d.version, 0) AS version, t.updated_at AS todo_updated_at, d.updated_at AS progress_updated_at").
		Order("COALESCE(d.updated_at,t.updated_at) DESC, t.id DESC").Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		updatedAt := r.TodoUpdatedAt
		if r.ProgressUpdatedAt != nil {
			updatedAt = *r.ProgressUpdatedAt
		}
		page.Items = append(page.Items, View{ID: r.ID, Title: r.Title, SourceQuote: short(r.SourceQuote), Summary: summary(r.Content), ClosedAt: r.ClosedAt, Version: r.Version, UpdatedAt: updatedAt})
	}
	return page, nil
}

func (s *Service) todo(ctx context.Context, id uint64) (*domain.Todo, error) {
	var t domain.Todo
	if err := s.db.WithContext(ctx).Where("id = ? AND action_type = ?", id, ActionType).First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (s *Service) Get(ctx context.Context, id uint64) (*View, error) {
	t, err := s.todo(ctx, id)
	if err != nil {
		return nil, err
	}
	v := &View{ID: t.ID, Title: t.Title, SourceQuote: t.SourceQuote, SourcePayload: json.RawMessage(t.Content), UpdatedAt: t.UpdatedAt}
	var p domain.DelegationProgress
	err = s.db.WithContext(ctx).First(&p, "todo_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	v.Content = json.RawMessage(p.Content)
	v.Summary = summary(p.Content)
	v.ClosedAt = p.ClosedAt
	v.Version = p.Version
	v.UpdatedAt = p.UpdatedAt
	return v, nil
}

type UpdateInput struct {
	ExpectedVersion *int32          `json:"expected_version"`
	Content         json.RawMessage `json:"content"`
	Closed          *bool           `json:"closed"`
	Actor           string          `json:"actor"`
}

// Update writes only the check result and records it on the existing Todo event
// stream. Neither the frozen context nor Todo/Task execution status is changed.
func (s *Service) Update(ctx context.Context, id uint64, in UpdateInput) (*View, error) {
	if in.ExpectedVersion == nil || *in.ExpectedVersion < 0 || !json.Valid(in.Content) || strings.TrimSpace(in.Actor) == "" {
		return nil, ErrInvalid
	}
	t, err := s.todo(ctx, id)
	if err != nil {
		return nil, err
	}
	old, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if old.Version != *in.ExpectedVersion {
		return nil, ErrConflict
	}
	now := time.Now().UTC()
	closedAt := old.ClosedAt
	if in.Closed != nil {
		if *in.Closed {
			if closedAt == nil {
				closedAt = &now
			}
		} else {
			closedAt = nil
		}
	}
	p := domain.DelegationProgress{TodoID: id, Content: datatypes.JSON(in.Content), ClosedAt: closedAt, Version: old.Version + 1, CreatedAt: now, UpdatedAt: now}
	var write *gorm.DB
	if old.Version == 0 {
		write = s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&p)
	} else {
		write = s.db.WithContext(ctx).Model(&domain.DelegationProgress{}).Where("todo_id = ? AND version = ?", id, old.Version).
			Updates(map[string]any{"content": p.Content, "closed_at": closedAt, "version": p.Version, "updated_at": now})
	}
	if write.Error != nil {
		return nil, write.Error
	}
	if write.RowsAffected != 1 {
		return nil, ErrConflict
	}
	detail, err := json.Marshal(map[string]any{"event_type": "delegation_checked", "content": in.Content, "closed_at": closedAt, "version": p.Version})
	if err != nil {
		return nil, err
	}
	snapshot, err := domain.EncodeTodoEventSnapshot(t)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(&domain.TodoEvent{TodoID: id, ToStatus: t.Status, Actor: in.Actor, Detail: detail, Snapshot: snapshot}).Error; err != nil {
		return nil, fmt.Errorf("progress saved but audit failed: %w", err)
	}
	return s.Get(ctx, id)
}

type CheckTask struct {
	ID        uint64    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Summary   *string   `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// Tasks finds the first materialized check and later ordinary Tasks carrying
// source.delegation_id. This is a query association, never a state synchronizer.
func (s *Service) Tasks(ctx context.Context, id uint64, page, limit int) ([]CheckTask, error) {
	if page < 1 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	if _, err := s.todo(ctx, id); err != nil {
		return nil, err
	}
	items := []CheckTask{}
	err := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("todo_id = ? OR json_extract(source_payload, '$.source.delegation_id') = ? OR json_extract(source_payload, '$.annotation.delegation_id') = ?", id, id, id).
		Select("id,title,status,summary,created_at").Order("id DESC").Offset((page - 1) * limit).Limit(limit).Scan(&items).Error
	return items, err
}

func summary(raw []byte) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return short(text)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) == nil && json.Unmarshal(fields["summary"], &text) == nil {
		return short(text)
	}
	if len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "已记录核验信息，查看详情"
	}
	return ""
}
func short(s string) string {
	r := []rune(s)
	if len(r) > 300 {
		return string(r[:300]) + "…"
	}
	return s
}
