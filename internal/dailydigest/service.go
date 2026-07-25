package dailydigest

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jarvis/internal/observability"

	"gorm.io/gorm"
)

// Options 是 Service 的构造参数。
type Options struct {
	DB              *gorm.DB
	Location        *time.Location
	Runner          SummaryRunner // 个人/群共用官方 codex（danger-full-access + 联网）
	PrincipalOpenID string        // person scope 的 scope_id
	GitAuthor       string        // 供个人 prompt 引导 git log
	RepoRoot        string        // 已 clone 仓库的根目录
	WorkspaceRoot   string        // Jarvis 仓库根目录；按日 Markdown 的持久化根
	PersonSkillDir  string        // summarize-person-day Skill 目录
	GroupSkillDir   string        // feishu-group-daily-summary Skill 目录
	SummarySandbox  string        // 两类总结的 codex sandbox（danger-full-access）
	GroupMsgLimit   int           // 每群每天喂进 prompt 的消息上限
	GroupConcur     int           // 批量群总结的并发上限（>=1）
}

// Service 编排每日总结。定时和手动只负责触发，抢占、证据收集、生成、落库共用。
type Service struct {
	store           *Store
	person          *personGenerator
	group           *groupGenerator
	db              *gorm.DB
	location        *time.Location
	principalOpenID string
	groupConcur     int
	logger          *log.Logger
	now             func() time.Time
}

func NewService(opts Options) (*Service, error) {
	if opts.DB == nil {
		return nil, fmt.Errorf("daily digest service db is nil")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("daily digest service location is nil")
	}
	if opts.Runner == nil {
		return nil, fmt.Errorf("daily digest service summary runner is nil")
	}
	if strings.TrimSpace(opts.PrincipalOpenID) == "" {
		return nil, fmt.Errorf("daily digest service principal_open_id is required")
	}
	if strings.TrimSpace(opts.GitAuthor) == "" {
		return nil, fmt.Errorf("daily digest service git author is required")
	}
	if strings.TrimSpace(opts.RepoRoot) == "" {
		return nil, fmt.Errorf("daily digest service repo root is required")
	}
	workspaceRoot, err := filepath.Abs(strings.TrimSpace(opts.WorkspaceRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve daily digest workspace root: %w", err)
	}
	if strings.TrimSpace(opts.WorkspaceRoot) == "" {
		return nil, fmt.Errorf("daily digest service workspace root is required")
	}
	if stat, err := os.Stat(workspaceRoot); err != nil {
		return nil, fmt.Errorf("stat daily digest workspace root %q: %w", workspaceRoot, err)
	} else if !stat.IsDir() {
		return nil, fmt.Errorf("daily digest workspace root %q is not a directory", workspaceRoot)
	}
	personSkillDir, err := filepath.Abs(strings.TrimSpace(opts.PersonSkillDir))
	if err != nil {
		return nil, fmt.Errorf("resolve personal summary skill directory: %w", err)
	}
	skillText, err := loadPersonSummarySkill(personSkillDir)
	if err != nil {
		return nil, err
	}
	groupSkillText, err := loadGroupSummarySkill(opts.GroupSkillDir)
	if err != nil {
		return nil, err
	}
	switch opts.SummarySandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("daily digest service summary sandbox must be read-only/workspace-write/danger-full-access, got %q", opts.SummarySandbox)
	}
	if opts.GroupMsgLimit <= 0 {
		return nil, fmt.Errorf("daily digest service group message limit must be positive")
	}
	if opts.GroupConcur < 1 {
		return nil, fmt.Errorf("daily digest service group concurrency must be >= 1")
	}
	store, err := NewStore(opts.DB, opts.Location)
	if err != nil {
		return nil, err
	}
	return &Service{
		store: store,
		person: &personGenerator{
			db:              opts.DB,
			runner:          opts.Runner,
			location:        opts.Location,
			principalOpenID: opts.PrincipalOpenID,
			gitAuthor:       opts.GitAuthor,
			repoRoot:        opts.RepoRoot,
			workspaceRoot:   workspaceRoot,
			skillDir:        personSkillDir,
			skillText:       skillText,
			sandbox:         opts.SummarySandbox,
		},
		group: &groupGenerator{
			db:           opts.DB,
			runner:       opts.Runner,
			location:     opts.Location,
			messageLimit: opts.GroupMsgLimit,
			skillText:    groupSkillText,
			sandbox:      opts.SummarySandbox,
		},
		db:              opts.DB,
		location:        opts.Location,
		principalOpenID: opts.PrincipalOpenID,
		groupConcur:     opts.GroupConcur,
		logger:          log.New(log.Writer(), "dailydigest ", log.LstdFlags|log.Lmicroseconds),
		now:             time.Now,
	}, nil
}

// dayBounds 把日期解析成本地自然日，并拒绝未来日期。
func (s *Service) dayBounds(date string) (start, end time.Time, err error) {
	start, err = s.store.dayStart(date)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	today, err := s.store.dayStart(s.today())
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if start.After(today) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: digest date %s is in the future", ErrInvalidInput, date)
	}
	return start, start.AddDate(0, 0, 1), nil
}

func (s *Service) today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

func (s *Service) cutoffForDay(dayEnd time.Time) time.Time {
	now := s.now().In(s.location)
	if now.Before(dayEnd) {
		return now
	}
	return dayEnd.Add(-time.Nanosecond)
}

func (s *Service) ListByDate(ctx context.Context, date string) ([]DigestView, error) {
	if strings.TrimSpace(date) == "" {
		date = s.today()
	}
	return s.store.ListByDate(ctx, date)
}

func (s *Service) validateTarget(ctx context.Context, scope, scopeID, date string) (*keyGroup, error) {
	if err := validateScope(scope, scopeID); err != nil {
		return nil, err
	}
	if _, _, err := s.dayBounds(date); err != nil {
		return nil, err
	}
	if scope == ScopePerson {
		if scopeID != s.principalOpenID {
			return nil, fmt.Errorf("%w: person scope_id %q must be principal open_id", ErrInvalidInput, scopeID)
		}
		return nil, nil
	}
	groups, err := loadKeyGroups(ctx, s.db)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].ScopeID == scopeID {
			return &groups[i], nil
		}
	}
	return nil, fmt.Errorf("%w: group scope_id %q is not a key group", ErrInvalidInput, scopeID)
}

// GenerateOne 保留同步手动调用能力；页面使用 KickGenerateOne 异步调用。
func (s *Service) GenerateOne(ctx context.Context, scope, scopeID, date string) error {
	if strings.TrimSpace(date) == "" {
		date = s.today()
	}
	target, err := s.validateTarget(ctx, scope, scopeID, date)
	if err != nil {
		return err
	}
	if err := s.store.ClaimGeneration(ctx, scope, scopeID, date, TriggerManual, true); err != nil {
		return err
	}
	return s.runClaimed(ctx, scope, scopeID, date, target)
}

func (s *Service) runClaimed(ctx context.Context, scope, scopeID, date string, target *keyGroup) error {
	if scope == ScopePerson {
		return s.runClaimedPerson(ctx, scopeID, date)
	}
	if target == nil {
		return fmt.Errorf("run claimed group digest scope_id=%s without target", scopeID)
	}
	return s.runClaimedGroup(ctx, *target, date)
}

func (s *Service) runClaimedPerson(ctx context.Context, scopeID, date string) error {
	start, end, err := s.dayBounds(date)
	if err != nil {
		return err
	}
	cutoffAt := s.cutoffForDay(end)
	result, genErr := s.person.Generate(ctx, date, start, end, cutoffAt)
	if genErr != nil {
		if err := s.store.SetFailed(ctx, ScopePerson, scopeID, date, genErr.Error()); err != nil {
			return fmt.Errorf("record person digest failure: %w (original: %v)", err, genErr)
		}
		return genErr
	}
	return s.store.SetDone(ctx, ScopePerson, scopeID, date, result.Summary, result.SourceCount, result.Coverage, result.CutoffAt)
}

func (s *Service) runClaimedGroup(ctx context.Context, group keyGroup, date string) error {
	start, end, err := s.dayBounds(date)
	if err != nil {
		return err
	}
	cutoffAt := s.cutoffForDay(end)
	result, genErr := s.group.Generate(ctx, group.ID, group.Name, group.ChatID, date, start, end, cutoffAt)
	if genErr != nil {
		if err := s.store.SetFailed(ctx, ScopeGroup, group.ScopeID, date, genErr.Error()); err != nil {
			return fmt.Errorf("record group digest failure: %w (original: %v)", err, genErr)
		}
		return genErr
	}
	return s.store.SetDone(
		ctx,
		ScopeGroup,
		group.ScopeID,
		date,
		result.Summary,
		result.SourceCount,
		result.Coverage,
		result.CutoffAt,
	)
}

// GeneratePersonalScheduled 是个人总结唯一的 cron 入口。当天已有任何自动/手动
// 尝试都正常跳过；失败后只允许用户显式手动重试。
func (s *Service) GeneratePersonalScheduled(ctx context.Context, date string) (bool, error) {
	if strings.TrimSpace(date) == "" {
		date = s.today()
	}
	if _, err := s.validateTarget(ctx, ScopePerson, s.principalOpenID, date); err != nil {
		return false, err
	}
	err := s.store.ClaimGeneration(ctx, ScopePerson, s.principalOpenID, date, TriggerSchedule, false)
	if errors.Is(err, ErrAlreadyDone) ||
		errors.Is(err, ErrAlreadyGenerating) ||
		errors.Is(err, ErrAlreadyAttempted) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, s.runClaimedPerson(ctx, s.principalOpenID, date)
}

// GenerateForDate 保留原批量能力供显式调用；定时器不再调用它，避免个人总结和群
// 总结互相阻塞。批量任务也采用 schedule 语义，不覆盖当天已完成的结果。
func (s *Service) GenerateForDate(ctx context.Context, date string) error {
	if strings.TrimSpace(date) == "" {
		date = s.today()
	}
	var firstErr error
	if _, err := s.GeneratePersonalScheduled(ctx, date); err != nil {
		firstErr = err
	}
	groups, err := loadKeyGroups(ctx, s.db)
	if err != nil {
		if firstErr != nil {
			return firstErr
		}
		return err
	}
	sem := make(chan struct{}, s.groupConcur)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range groups {
		group := groups[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			err := s.store.ClaimGeneration(ctx, ScopeGroup, group.ScopeID, date, TriggerSchedule, false)
			if errors.Is(err, ErrAlreadyDone) ||
				errors.Is(err, ErrAlreadyGenerating) ||
				errors.Is(err, ErrAlreadyAttempted) {
				return
			}
			if err == nil {
				err = s.runClaimedGroup(ctx, group, date)
			}
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstErr
}

type KickResult struct {
	Scope       string `json:"scope"`
	ScopeID     string `json:"scope_id"`
	Date        string `json:"date"`
	Status      string `json:"status"`
	TriggerType string `json:"trigger_type"`
}

// KickGenerateOne 异步手动生成/重算。生成权在返回 HTTP 前完成抢占，跨浏览器、
// 手动与 cron 并发都会得到明确冲突。
func (s *Service) KickGenerateOne(ctx context.Context, scope, scopeID, date string) (*KickResult, error) {
	if strings.TrimSpace(date) == "" {
		date = s.today()
	}
	target, err := s.validateTarget(ctx, scope, scopeID, date)
	if err != nil {
		return nil, err
	}
	if err := s.store.ClaimGeneration(ctx, scope, scopeID, date, TriggerManual, true); err != nil {
		return nil, err
	}

	detached := observability.Detached(ctx)
	go func() {
		if err := s.runClaimed(detached, scope, scopeID, date, target); err != nil {
			s.logger.Printf("logid=%s background digest scope=%s scope_id=%s date=%s error=%+v", observability.LogID(detached), scope, scopeID, date, err)
		}
	}()
	return &KickResult{
		Scope: scope, ScopeID: scopeID, Date: date,
		Status: StatusGenerating, TriggerType: TriggerManual,
	}, nil
}

func (s *Service) RecoverInterrupted(ctx context.Context) (int64, error) {
	return s.store.RecoverInterruptedGeneration(ctx)
}
