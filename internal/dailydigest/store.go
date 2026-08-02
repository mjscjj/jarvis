// Package dailydigest 生成并缓存「每日进度总结」：个人（我）和关键群
// （is_key_group=1）都用 codex agent 调查并综合。一天一个 scope 一行，重算
// upsert 覆盖（见 docs/design-daily-digest.md）。
package dailydigest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"jarvis/internal/datatypes"
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

// engine 标注用哪个引擎生成，便于排查。个人与群统一使用官方 codex runner。
const EngineCodex = "codex"

// trigger 标明是谁启动了本轮生成；手动重算与 cron 共用同一套生成链路。
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
)

var (
	ErrInvalidInput      = errors.New("invalid daily digest input")
	ErrNotFound          = errors.New("daily digest not found")
	ErrAlreadyGenerating = errors.New("daily digest is already generating")
	ErrAlreadyDone       = errors.New("daily digest is already done")
	ErrAlreadyAttempted  = errors.New("daily digest already has an automatic attempt")
)

// SourceCoverageItem 让“查到了什么/哪路失败了”成为可观察数据，而不是藏在模型内部。
type SourceCoverageItem struct {
	Status string `json:"status"` // group: ok/empty/error; person: complete/partial/empty/error/unavailable
	Count  int    `json:"count"`
	Note   string `json:"note,omitempty"`
}

type SourceCoverage map[string]SourceCoverageItem

// DigestView 是一条每日总结的只读视图，供 API 输出。digest_date 显式格式化成
// YYYY-MM-DD（模型层 datatypes.Date 的 JSON 是完整时间戳，不适合直接透出）。
type DigestView struct {
	ID             uint64         `json:"id"`
	Scope          string         `json:"scope"`
	ScopeID        string         `json:"scope_id"`
	DigestDate     string         `json:"digest_date"` // YYYY-MM-DD（本地时区自然日）
	Summary        string         `json:"summary"`
	Status         string         `json:"status"`
	TriggerType    string         `json:"trigger_type"`
	SourceCount    int            `json:"source_count"`
	SourceCoverage SourceCoverage `json:"source_coverage"`
	Engine         string         `json:"engine"`
	ErrorDetail    *string        `json:"error_detail"`
	StartedAt      *string        `json:"started_at"`
	CutoffAt       *string        `json:"cutoff_at"`
	GeneratedAt    *string        `json:"generated_at"` // RFC3339；未完成时为 null
	UpdatedAt      string         `json:"updated_at"`   // RFC3339
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
	view, err := s.toView(&row)
	if err != nil {
		return nil, err
	}
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
		view, err := s.toView(&rows[i])
		if err != nil {
			return nil, err
		}
		views[i] = view
	}
	return views, nil
}

// ClaimGeneration 原子抢占某 scope 某天的生成权。手动触发可重算 done/failed；
// 定时触发只创建当天第一次尝试，已有 failed/pending 也不自动重跑。两者都不能
// 抢占 generating，避免手动与 cron 重复运行。
func (s *Store) ClaimGeneration(ctx context.Context, scope, scopeID, date, trigger string, force bool) error {
	if err := validateScope(scope, scopeID); err != nil {
		return err
	}
	if trigger != TriggerManual && trigger != TriggerSchedule {
		return fmt.Errorf("%w: trigger must be manual or schedule, got %q", ErrInvalidInput, trigger)
	}
	day, err := s.dayStart(date)
	if err != nil {
		return err
	}
	startedAt := time.Now()
	row := domain.DailyDigest{
		Scope:       scope,
		ScopeID:     scopeID,
		DigestDate:  datatypes.Date(day),
		Status:      StatusGenerating,
		TriggerType: trigger,
		Engine:      engineForScope(scope),
		StartedAt:   &startedAt,
	}
	create := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   uniqueScopeDateColumns(),
		DoNothing: true,
	}).Create(&row)
	if create.Error != nil {
		return fmt.Errorf("create daily digest claim scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, create.Error)
	}
	if create.RowsAffected == 1 {
		return nil
	}

	var current domain.DailyDigest
	if err := s.db.WithContext(ctx).
		Where("scope = ? AND scope_id = ? AND digest_date = ?", scope, scopeID, day).
		First(&current).Error; err != nil {
		return fmt.Errorf("inspect existing daily digest claim scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, err)
	}
	if !force {
		switch current.Status {
		case StatusGenerating:
			return fmt.Errorf("%w: scope=%s scope_id=%s date=%s", ErrAlreadyGenerating, scope, scopeID, date)
		case StatusDone:
			return fmt.Errorf("%w: scope=%s scope_id=%s date=%s", ErrAlreadyDone, scope, scopeID, date)
		default:
			return fmt.Errorf(
				"%w: scope=%s scope_id=%s date=%s status=%s",
				ErrAlreadyAttempted,
				scope,
				scopeID,
				date,
				current.Status,
			)
		}
	}

	query := s.db.WithContext(ctx).Model(&domain.DailyDigest{}).
		Where("scope = ? AND scope_id = ? AND digest_date = ?", scope, scopeID, day).
		Where("status <> ?", StatusGenerating)
	result := query.Updates(map[string]any{
		"status":          StatusGenerating,
		"trigger_type":    trigger,
		"engine":          engineForScope(scope),
		"error_detail":    nil,
		"started_at":      startedAt,
		"cutoff_at":       nil,
		"source_coverage": nil,
	})
	if result.Error != nil {
		return fmt.Errorf("update daily digest claim scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, result.Error)
	}
	if result.RowsAffected == 1 {
		return nil
	}

	if err := s.db.WithContext(ctx).
		Where("scope = ? AND scope_id = ? AND digest_date = ?", scope, scopeID, day).
		First(&current).Error; err != nil {
		return fmt.Errorf("inspect rejected daily digest claim scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, err)
	}
	switch current.Status {
	case StatusGenerating:
		return fmt.Errorf("%w: scope=%s scope_id=%s date=%s", ErrAlreadyGenerating, scope, scopeID, date)
	case StatusDone:
		return fmt.Errorf("%w: scope=%s scope_id=%s date=%s", ErrAlreadyDone, scope, scopeID, date)
	default:
		return fmt.Errorf("claim daily digest scope=%s scope_id=%s date=%s rejected in unexpected status %q", scope, scopeID, date, current.Status)
	}
}

// SetDone 写入生成成功的总结正文与来源计数，置 done、盖生成时刻、清 error。
func (s *Store) SetDone(ctx context.Context, scope, scopeID, date, summary string, sourceCount int, coverage SourceCoverage, cutoffAt time.Time) error {
	if err := validateScope(scope, scopeID); err != nil {
		return err
	}
	if strings.TrimSpace(summary) == "" {
		return fmt.Errorf("%w: done summary must be non-blank", ErrInvalidInput)
	}
	if sourceCount < 0 {
		return fmt.Errorf("%w: source_count must not be negative", ErrInvalidInput)
	}
	coverageJSON, err := json.Marshal(coverage)
	if err != nil {
		return fmt.Errorf("marshal daily digest source coverage: %w", err)
	}
	day, err := s.dayStart(date)
	if err != nil {
		return err
	}
	now := time.Now()
	result := s.db.WithContext(ctx).Model(&domain.DailyDigest{}).
		Where("scope = ? AND scope_id = ? AND digest_date = ?", scope, scopeID, day).
		Updates(map[string]any{
			"summary":         summary,
			"source_count":    sourceCount,
			"source_coverage": datatypes.JSON(coverageJSON),
			"status":          StatusDone,
			"engine":          engineForScope(scope),
			"error_detail":    nil,
			"cutoff_at":       cutoffAt,
			"generated_at":    now,
		})
	if result.Error != nil {
		return fmt.Errorf("set daily digest done scope=%s scope_id=%s date=%s: %w", scope, scopeID, date, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: set done scope=%s scope_id=%s date=%s affected=%d", ErrNotFound, scope, scopeID, date, result.RowsAffected)
	}
	return nil
}

// RecoverInterruptedGeneration 在进程启动时把遗留 generating 显式标失败。旧进程
// 已不存在，不可能继续完成；保留 started_at 供页面和日志排查。
func (s *Store) RecoverInterruptedGeneration(ctx context.Context) (int64, error) {
	detail := "generation interrupted by Jarvis process restart"
	result := s.db.WithContext(ctx).Model(&domain.DailyDigest{}).
		Where("status = ?", StatusGenerating).
		Updates(map[string]any{"status": StatusFailed, "error_detail": detail})
	if result.Error != nil {
		return 0, fmt.Errorf("recover interrupted daily digests: %w", result.Error)
	}
	return result.RowsAffected, nil
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

func (s *Store) toView(row *domain.DailyDigest) (DigestView, error) {
	coverage := SourceCoverage{}
	if len(row.SourceCoverage) > 0 {
		if err := json.Unmarshal(row.SourceCoverage, &coverage); err != nil {
			return DigestView{}, fmt.Errorf("decode daily digest id=%d source coverage: %w", row.ID, err)
		}
	}
	view := DigestView{
		ID:             row.ID,
		Scope:          row.Scope,
		ScopeID:        row.ScopeID,
		DigestDate:     time.Time(row.DigestDate).Format("2006-01-02"),
		Summary:        row.Summary,
		Status:         row.Status,
		TriggerType:    row.TriggerType,
		SourceCount:    row.SourceCount,
		SourceCoverage: coverage,
		Engine:         row.Engine,
		ErrorDetail:    row.ErrorDetail,
		UpdatedAt:      row.UpdatedAt.In(s.location).Format(time.RFC3339),
	}
	if row.StartedAt != nil {
		started := row.StartedAt.In(s.location).Format(time.RFC3339)
		view.StartedAt = &started
	}
	if row.CutoffAt != nil {
		cutoff := row.CutoffAt.In(s.location).Format(time.RFC3339)
		view.CutoffAt = &cutoff
	}
	if row.GeneratedAt != nil {
		generated := row.GeneratedAt.In(s.location).Format(time.RFC3339)
		view.GeneratedAt = &generated
	}
	return view, nil
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
	return EngineCodex
}

func uniqueScopeDateColumns() []clause.Column {
	return []clause.Column{{Name: "scope"}, {Name: "scope_id"}, {Name: "digest_date"}}
}
