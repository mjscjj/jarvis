package dailydigest

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Options 是 Service 的构造参数。
type Options struct {
	DB              *gorm.DB
	Location        *time.Location
	PersonRunner    PersonRunner    // codex（danger-full-access + 联网）
	GroupRunner     GroupTextRunner // qwen 纯文本
	PrincipalOpenID string          // person scope 的 scope_id
	GitAuthor       string          // 供个人 prompt 引导 git log
	PersonSandbox   string          // 个人总结 codex sandbox（danger-full-access）
	GroupMsgLimit   int             // 每群每天喂进 prompt 的消息上限
	GroupConcur     int             // 一轮批量生成里群总结的并发上限（>=1）
}

// Service 编排每日总结：批量生成当天全部 scope、单条生成/重算、异步 kick、读取。
type Service struct {
	store           *Store
	person          *personGenerator
	group           *groupGenerator
	db              *gorm.DB
	location        *time.Location
	principalOpenID string
	groupConcur     int
	logger          *log.Logger
}

func NewService(opts Options) (*Service, error) {
	if opts.DB == nil {
		return nil, fmt.Errorf("daily digest service db is nil")
	}
	if opts.Location == nil {
		return nil, fmt.Errorf("daily digest service location is nil")
	}
	if opts.PersonRunner == nil {
		return nil, fmt.Errorf("daily digest service person runner is nil")
	}
	if opts.GroupRunner == nil {
		return nil, fmt.Errorf("daily digest service group runner is nil")
	}
	if strings.TrimSpace(opts.PrincipalOpenID) == "" {
		return nil, fmt.Errorf("daily digest service principal_open_id is required")
	}
	if strings.TrimSpace(opts.GitAuthor) == "" {
		return nil, fmt.Errorf("daily digest service git author is required")
	}
	switch opts.PersonSandbox {
	case "read-only", "workspace-write", "danger-full-access":
	default:
		return nil, fmt.Errorf("daily digest service person sandbox must be read-only/workspace-write/danger-full-access, got %q", opts.PersonSandbox)
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
			runner:          opts.PersonRunner,
			location:        opts.Location,
			principalOpenID: opts.PrincipalOpenID,
			gitAuthor:       opts.GitAuthor,
			sandbox:         opts.PersonSandbox,
		},
		group: &groupGenerator{
			db:           opts.DB,
			runner:       opts.GroupRunner,
			location:     opts.Location,
			messageLimit: opts.GroupMsgLimit,
		},
		db:              opts.DB,
		location:        opts.Location,
		principalOpenID: opts.PrincipalOpenID,
		groupConcur:     opts.GroupConcur,
		logger:          log.New(log.Writer(), "dailydigest ", log.LstdFlags|log.Lmicroseconds),
	}, nil
}

// dayBounds 把日期字符串解析成本地时区 [00:00, 次日 00:00)。
func (s *Service) dayBounds(date string) (start, end time.Time, err error) {
	start, err = s.store.dayStart(date)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, start.AddDate(0, 0, 1), nil
}

// today 返回本地时区今天的日期字符串。
func (s *Service) today() string {
	return time.Now().In(s.location).Format("2006-01-02")
}

// ListByDate 读某天全部 scope 的总结（个人 + 各关键群），date 为空取今天。
func (s *Service) ListByDate(ctx context.Context, date string) ([]DigestView, error) {
	if strings.TrimSpace(date) == "" {
		date = s.today()
	}
	return s.store.ListByDate(ctx, date)
}

// GenerateOne 同步生成/重算单条（某 scope 某天）。先置 generating，跑对应引擎，
// 成功置 done、失败置 failed。cron 批量与异步 kick 都复用它。
func (s *Service) GenerateOne(ctx context.Context, scope, scopeID, date string) error {
	if err := validateScope(scope, scopeID); err != nil {
		return err
	}
	if scope == ScopeGroup {
		return s.generateGroupByScopeID(ctx, scopeID, date)
	}
	return s.generatePerson(ctx, scopeID, date)
}

func (s *Service) generatePerson(ctx context.Context, scopeID, date string) error {
	if scopeID != s.principalOpenID {
		return fmt.Errorf("%w: person scope_id %q must be principal open_id", ErrInvalidInput, scopeID)
	}
	start, end, err := s.dayBounds(date)
	if err != nil {
		return err
	}
	if err := s.store.SetGenerating(ctx, ScopePerson, scopeID, date); err != nil {
		return err
	}
	summary, count, genErr := s.person.Generate(ctx, date, start, end)
	if genErr != nil {
		if err := s.store.SetFailed(ctx, ScopePerson, scopeID, date, genErr.Error()); err != nil {
			return fmt.Errorf("record person digest failure: %w (original: %v)", err, genErr)
		}
		return genErr
	}
	return s.store.SetDone(ctx, ScopePerson, scopeID, date, summary, count)
}

// generateGroupByScopeID 生成单个关键群总结。scopeID 必须对应一个存在的关键群。
func (s *Service) generateGroupByScopeID(ctx context.Context, scopeID, date string) error {
	groups, err := loadKeyGroups(ctx, s.db)
	if err != nil {
		return err
	}
	var target *keyGroup
	for i := range groups {
		if groups[i].ScopeID == scopeID {
			target = &groups[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("%w: group scope_id %q is not a key group", ErrInvalidInput, scopeID)
	}
	return s.generateGroup(ctx, *target, date)
}

func (s *Service) generateGroup(ctx context.Context, kg keyGroup, date string) error {
	start, end, err := s.dayBounds(date)
	if err != nil {
		return err
	}
	if err := s.store.SetGenerating(ctx, ScopeGroup, kg.ScopeID, date); err != nil {
		return err
	}
	summary, count, genErr := s.group.Generate(ctx, kg.ID, kg.Name, kg.ChatID, date, start, end)
	if genErr != nil {
		if err := s.store.SetFailed(ctx, ScopeGroup, kg.ScopeID, date, genErr.Error()); err != nil {
			return fmt.Errorf("record group digest failure: %w (original: %v)", err, genErr)
		}
		return genErr
	}
	return s.store.SetDone(ctx, ScopeGroup, kg.ScopeID, date, summary, count)
}

// GenerateForDate 批量生成某天全部总结：个人 1 条 + 每个关键群各 1 条。date 为空
// 取今天。群总结有界并发；个人 1 条串行跑。单条失败不阻断其余（已各自落 failed），
// 汇总首个错误返回，便于 cron 日志暴露问题。
func (s *Service) GenerateForDate(ctx context.Context, date string) error {
	if strings.TrimSpace(date) == "" {
		date = s.today()
	}
	// 个人：codex 慢，串行先跑。失败只记录不 return，继续跑群。
	var firstErr error
	if err := s.generatePerson(ctx, s.principalOpenID, date); err != nil {
		s.logger.Printf("person digest date=%s error=%v", date, err)
		firstErr = err
	}

	groups, err := loadKeyGroups(ctx, s.db)
	if err != nil {
		if firstErr == nil {
			firstErr = err
		}
		return firstErr
	}

	sem := make(chan struct{}, s.groupConcur)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range groups {
		kg := groups[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.generateGroup(ctx, kg, date); err != nil {
				s.logger.Printf("group digest group_id=%d date=%s error=%v", kg.ID, date, err)
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

// KickResult 是异步触发的即时返回：告诉前端已进入 generating，去轮询。
type KickResult struct {
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id"`
	Date    string `json:"date"`
	Status  string `json:"status"`
}

// KickGenerateOne 异步生成/重算单条：同步置 status=generating 后立即返回，后台
// goroutine 跑实际生成（个人 codex 慢，必须异步），完成置 done/failed。参考 M5
// KickExecute 的「同步抢占 + 后台跑」。date 为空取今天。
func (s *Service) KickGenerateOne(ctx context.Context, scope, scopeID, date string) (*KickResult, error) {
	if err := validateScope(scope, scopeID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(date) == "" {
		date = s.today()
	}
	// 先校验入参（日期可解析、group 存在、person 是 principal），并同步置 generating，
	// 让前端立刻看到状态；任一校验失败 fail-fast 返回，不起后台任务。
	if _, _, err := s.dayBounds(date); err != nil {
		return nil, err
	}
	if scope == ScopePerson {
		if scopeID != s.principalOpenID {
			return nil, fmt.Errorf("%w: person scope_id %q must be principal open_id", ErrInvalidInput, scopeID)
		}
	} else {
		groups, err := loadKeyGroups(ctx, s.db)
		if err != nil {
			return nil, err
		}
		found := false
		for i := range groups {
			if groups[i].ScopeID == scopeID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: group scope_id %q is not a key group", ErrInvalidInput, scopeID)
		}
	}
	if err := s.store.SetGenerating(ctx, scope, scopeID, date); err != nil {
		return nil, err
	}

	// 后台跑实际生成。用独立 context（脱离 HTTP 请求生命周期），generateXxx 内部会
	// 再置一次 generating（幂等），并在结束时落 done/failed。
	go func() {
		bgCtx := context.Background()
		var err error
		if scope == ScopeGroup {
			err = s.generateGroupByScopeID(bgCtx, scopeID, date)
		} else {
			err = s.generatePerson(bgCtx, scopeID, date)
		}
		if err != nil {
			s.logger.Printf("background digest scope=%s scope_id=%s date=%s error=%v", scope, scopeID, date, err)
		}
	}()

	return &KickResult{Scope: scope, ScopeID: scopeID, Date: date, Status: StatusGenerating}, nil
}
