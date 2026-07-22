// Package dailydigest 生成并缓存「每日进度总结」：个人（我）当天进度用 codex agent
// 综合，关键群（is_key_group=1）当天进度用 qwen 单次调用归纳。一天一个 scope 一行，
// 重算 upsert 覆盖（见 docs/design-daily-digest.md）。
package dailydigest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// scope 取值。person 全局一条（scope_id=principal open_id）；group 每个关键群一条
// （scope_id=feishu_group.id 的字符串）。
const (
	ScopePerson = "person"
	ScopeGroup  = "group"
)

// status 状态机：pending（占位未生成）→ generating（生成中）→ done/failed。
const (
	StatusPending    = "pending"
	StatusGenerating = "generating"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

// engine 标注用哪个引擎生成，便于排查。
const (
	EnginePerson = "codex"
	EngineGroup  = "qwen"
)

var (
	ErrInvalidInput = errors.New("invalid daily digest input")
	ErrNotFound     = errors.New("daily digest not found")
)

// DigestView 是一条每日总结的只读视图，供 API 输出。digest_date 显式格式化成
// YYYY-MM-DD（模型层 datatypes.Date 的 JSON 是完整时间戳，不适合直接透出）。
type DigestView struct {
	ID          uint64  `json:"id"`
	Scope       string  `json:"scope"`
	ScopeID     string  `json:"scope_id"`
	DigestDate  string  `json:"digest_date"` // YYYY-MM-DD（本地时区自然日）
	Summary     string  `json:"summary"`
	Status      string  `json:"status"`
	SourceCount int     `json:"source_count"`
	Engine      string  `json:"engine"`
	ErrorDetail *string `json:"error_detail"`
	GeneratedAt *string `json:"generated_at"` // RFC3339；未完成时为 null
	UpdatedAt   string  `json:"updated_at"`   // RFC3339
}

// Store 负责 daily_digest 表的读写。单用户本地低频，无事务、fail-fast。
type Store struct {
	db       *gorm.DB
	location *time.Location
}

func NewStore(db *gorm.DB, location *time.Location) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("daily digest store db is nil")
	}
	if location == nil {
		return nil, fmt.Errorf("daily digest store location is nil")
	}
	return &Store{db: db, location: location}, nil
}

// dayStart 把日期字符串（YYYY-MM-DD）解析成本地时区当天 00:00。校验 fail-fast。
func (s *Store) dayStart(date string) (time.Time, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return time.Time{}, fmt.Errorf("%w: digest date is required", ErrInvalidInput)
	}
	day, err := time.ParseInLocation("2006-01-02", date, s.location)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: digest date %q must be YYYY-MM-DD", ErrInvalidInput, date)
	}
	return day, nil
}

// GetByScopeDate 读某个 scope 某天的总结。不存在返回 (nil, nil)，由上层决定是否
// 视为「未生成」。
func (s *Store) GetByScopeDate(ctx context.Context, scope, scopeID, date string) (*DigestView, error) {
	if err := validateScope(scope, scopeID); err != nil {
		return nil, err
	}
	day, err := s.dayStart(date)
	if err != nil {
		return nil, err
	}
	var row domain.DailyDigest
	err = s.db.WithContext(ctx).
		Where("scope = ? AND scope_id = ? AND digest_date = ?", scope, scopeID, day).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load daily digest scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, err)
	}
	view := s.toView(&row)
	return &view, nil
}

// ListByDate 读某天全部 scope 的总结（个人 + 各关键群），按 scope、scope_id 稳定排序。
func (s *Store) ListByDate(ctx context.Context, date string) ([]DigestView, error) {
	day, err := s.dayStart(date)
	if err != nil {
		return nil, err
	}
	var rows []domain.DailyDigest
	if err := s.db.WithContext(ctx).
		Where("digest_date = ?", day).
		Order("scope ASC, scope_id ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list daily digests date=%s: %w", date, err)
	}
	views := make([]DigestView, len(rows))
	for i := range rows {
		views[i] = s.toView(&rows[i])
	}
	return views, nil
}

// SetGenerating 把某 scope 某天置为 generating（异步生成的起点）。若行不存在则新建
// 一条占位行，存在则覆盖 status 并清空上次 error。engine 随 scope 固定。
func (s *Store) SetGenerating(ctx context.Context, scope, scopeID, date string) error {
	if err := validateScope(scope, scopeID); err != nil {
		return err
	}
	day, err := s.dayStart(date)
	if err != nil {
		return err
	}
	row := domain.DailyDigest{
		Scope:      scope,
		ScopeID:    scopeID,
		DigestDate: datatypes.Date(day),
		Status:     StatusGenerating,
		Engine:     engineForScope(scope),
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   uniqueScopeDateColumns(),
		DoUpdates: clause.AssignmentColumns([]string{"status", "engine", "error_detail", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("set daily digest generating scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, err)
	}
	// OnConflict 更新路径不会清空 error_detail（AssignmentColumns 只覆盖到新值，
	// 新建行的 error_detail 是零值 nil），显式再清一次，避免旧失败信息残留。
	if err := s.db.WithContext(ctx).Model(&domain.DailyDigest{}).
		Where("scope = ? AND scope_id = ? AND digest_date = ?", scope, scopeID, day).
		Update("error_detail", nil).Error; err != nil {
		return fmt.Errorf("clear daily digest error scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, err)
	}
	return nil
}

// SetDone 写入生成成功的总结正文与来源计数，置 done、盖生成时刻、清 error。
func (s *Store) SetDone(ctx context.Context, scope, scopeID, date, summary string, sourceCount int) error {
	if err := validateScope(scope, scopeID); err != nil {
		return err
	}
	if strings.TrimSpace(summary) == "" {
		return fmt.Errorf("%w: done summary must be non-blank", ErrInvalidInput)
	}
	if sourceCount < 0 {
		return fmt.Errorf("%w: source_count must not be negative", ErrInvalidInput)
	}
	day, err := s.dayStart(date)
	if err != nil {
		return err
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&domain.DailyDigest{}).
		Where("scope = ? AND scope_id = ? AND digest_date = ?", scope, scopeID, day).
		Updates(map[string]any{
			"summary":      summary,
			"source_count": sourceCount,
			"status":       StatusDone,
			"engine":       engineForScope(scope),
			"error_detail": nil,
			"generated_at": now,
		})
	if result.Error != nil {
		return fmt.Errorf("set daily digest done scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: set done scope=%s scope_id=%s date=%s affected=%d", ErrNotFound, scope, scopeID, date, result.RowsAffected)
	}
	return nil
}

// SetFailed 记录失败原因并置 failed。
func (s *Store) SetFailed(ctx context.Context, scope, scopeID, date, detail string) error {
	if err := validateScope(scope, scopeID); err != nil {
		return err
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return fmt.Errorf("%w: failed detail must be non-blank", ErrInvalidInput)
	}
	day, err := s.dayStart(date)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&domain.DailyDigest{}).
		Where("scope = ? AND scope_id = ? AND digest_date = ?", scope, scopeID, day).
		Updates(map[string]any{
			"status":       StatusFailed,
			"error_detail": detail,
		})
	if result.Error != nil {
		return fmt.Errorf("set daily digest failed scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: set failed scope=%s scope_id=%s date=%s affected=%d", ErrNotFound, scope, scopeID, date, result.RowsAffected)
	}
	return nil
}

func (s *Store) toView(row *domain.DailyDigest) DigestView {
	view := DigestView{
		ID:          row.ID,
		Scope:       row.Scope,
		ScopeID:     row.ScopeID,
		DigestDate:  time.Time(row.DigestDate).Format("2006-01-02"),
		Summary:     row.Summary,
		Status:      row.Status,
		SourceCount: row.SourceCount,
		Engine:      row.Engine,
		ErrorDetail: row.ErrorDetail,
		UpdatedAt:   row.UpdatedAt.In(s.location).Format(time.RFC3339),
	}
	if row.GeneratedAt != nil {
		generated := row.GeneratedAt.In(s.location).Format(time.RFC3339)
		view.GeneratedAt = &generated
	}
	return view
}

func validateScope(scope, scopeID string) error {
	if scope != ScopePerson && scope != ScopeGroup {
		return fmt.Errorf("%w: scope must be person or group, got %q", ErrInvalidInput, scope)
	}
	if strings.TrimSpace(scopeID) == "" {
		return fmt.Errorf("%w: scope_id is required", ErrInvalidInput)
	}
	return nil
}

func engineForScope(scope string) string {
	if scope == ScopePerson {
		return EnginePerson
	}
	return EngineGroup
}

func uniqueScopeDateColumns() []clause.Column {
	return []clause.Column{{Name: "scope"}, {Name: "scope_id"}, {Name: "digest_date"}}
}
