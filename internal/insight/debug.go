package insight

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// DebugService backs the admin Debug panel: dependency health probes, table
// counts, pipeline backlog, per-module cron run history (parsed from logs),
// capture scan history, extraction watermarks, and recent todo/task detail.
// Everything is read on demand; it owns no state.
type DebugService struct {
	db         *gorm.DB
	mem0URL    string
	qdrantURL  string
	logs       *LogReader
	httpClient *http.Client
}

func NewDebugService(db *gorm.DB, mem0BaseURL, qdrantHost string, qdrantHTTPPort int, logs *LogReader) (*DebugService, error) {
	if db == nil {
		return nil, fmt.Errorf("debug service db is nil")
	}
	if logs == nil {
		return nil, fmt.Errorf("debug service log reader is nil")
	}
	if qdrantHTTPPort <= 0 {
		qdrantHTTPPort = 6333
	}
	return &DebugService{
		db:         db,
		mem0URL:    mem0BaseURL,
		qdrantURL:  fmt.Sprintf("http://%s:%d", qdrantHost, qdrantHTTPPort),
		logs:       logs,
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}, nil
}

// Dependency is one probed dependency's status.
type Dependency struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | error
	Detail string `json:"detail,omitempty"`
}

// TableCount is one table's row count.
type TableCount struct {
	Table string `json:"table"`
	Count int64  `json:"count"`
}

// BacklogMetric is one named pipeline backlog gauge (待处理积压量).
type BacklogMetric struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Value  int64  `json:"value"`
	Detail string `json:"detail,omitempty"`
}

// DebugStatus is the health sub-tab payload.
type DebugStatus struct {
	Time         string        `json:"time"`
	Dependencies []Dependency  `json:"dependencies"`
	Tables       []TableCount  `json:"tables"`
	Backlog      []BacklogMetric `json:"backlog"`
	TodoByStatus []StatusCount `json:"todo_by_status"`
	TaskByStatus []StatusCount `json:"task_by_status"`
}

func (s *DebugService) Status(ctx context.Context) *DebugStatus {
	status := &DebugStatus{Time: time.Now().Format(time.RFC3339)}
	status.Dependencies = []Dependency{
		s.probeMySQL(ctx),
		s.probeHTTP(ctx, "qdrant", s.qdrantURL+"/healthz"),
		s.probeHTTP(ctx, "mem0", s.mem0URL+"/health"),
	}
	status.Tables = s.tableCounts(ctx)
	status.Backlog = s.backlog(ctx)
	status.TodoByStatus = s.statusBreakdown(ctx, &domain.Todo{})
	status.TaskByStatus = s.statusBreakdown(ctx, &domain.Task{})
	return status
}

// backlog reports the "还没被下一环节处理" counts, so it's obvious at a glance
// where the pipeline is stuck: messages not yet memorized, todos awaiting my
// action, todos still open, tasks queued to execute.
func (s *DebugService) backlog(ctx context.Context) []BacklogMetric {
	metrics := []backlogSpec{
		{key: "msg_unmemorized", label: "未记忆化消息", detail: "M2 待处理", where: func(db *gorm.DB) *gorm.DB {
			return db.Model(&domain.Message{}).Where("mem0_processed = ?", false)
		}},
		{key: "todo_pending", label: "待我处理 Todo", detail: "need_info/need_decision", where: func(db *gorm.DB) *gorm.DB {
			return db.Model(&domain.Todo{}).Where("status IN ?", []string{"need_info", "need_decision"})
		}},
		{key: "todo_open", label: "未闭环 Todo", where: func(db *gorm.DB) *gorm.DB {
			return db.Model(&domain.Todo{}).Where("status IN ?", openTodoStatuses)
		}},
		{key: "todo_leader_open", label: "leader 交办未闭环", where: func(db *gorm.DB) *gorm.DB {
			return db.Model(&domain.Todo{}).Where("is_leader_assigned = ? AND status IN ?", true, openTodoStatuses)
		}},
		{key: "task_pending", label: "待执行 Task", where: func(db *gorm.DB) *gorm.DB {
			return db.Model(&domain.Task{}).Where("status IN ?", []string{"pending", "executing"})
		}},
	}
	out := make([]BacklogMetric, 0, len(metrics))
	for _, m := range metrics {
		var count int64
		if err := m.where(s.db.WithContext(ctx)).Count(&count).Error; err != nil {
			count = -1
		}
		out = append(out, BacklogMetric{Key: m.key, Label: m.label, Value: count, Detail: m.detail})
	}
	return out
}

// backlogSpec is an internal helper carrying the query builder for one metric.
type backlogSpec struct {
	key    string
	label  string
	detail string
	where  func(*gorm.DB) *gorm.DB
}

func (s *DebugService) statusBreakdown(ctx context.Context, model any) []StatusCount {
	rows := []StatusCount{}
	if err := s.db.WithContext(ctx).Model(model).
		Select("status, COUNT(*) AS count").Group("status").Order("count DESC").
		Scan(&rows).Error; err != nil {
		return []StatusCount{}
	}
	return rows
}

func (s *DebugService) probeMySQL(ctx context.Context) Dependency {
	sqlDB, err := s.db.DB()
	if err != nil {
		return Dependency{Name: "mysql", Status: "error", Detail: err.Error()}
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		return Dependency{Name: "mysql", Status: "error", Detail: err.Error()}
	}
	return Dependency{Name: "mysql", Status: "ok"}
}

func (s *DebugService) probeHTTP(ctx context.Context, name, url string) Dependency {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Dependency{Name: name, Status: "error", Detail: err.Error()}
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return Dependency{Name: name, Status: "error", Detail: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Dependency{Name: name, Status: "error", Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	return Dependency{Name: name, Status: "ok"}
}

func (s *DebugService) tableCounts(ctx context.Context) []TableCount {
	specs := []struct {
		table string
		model any
	}{
		{"message", &domain.Message{}},
		{"feishu_group", &domain.Group{}},
		{"todo", &domain.Todo{}},
		{"task", &domain.Task{}},
		{"person", &domain.Person{}},
		{"project", &domain.Project{}},
		{"scan_record", &domain.ScanRecord{}},
	}
	counts := make([]TableCount, 0, len(specs))
	for _, spec := range specs {
		var count int64
		// 单表 count 失败不阻断其他表，置 -1 表示读取失败。
		if err := s.db.WithContext(ctx).Model(spec.model).Count(&count).Error; err != nil {
			count = -1
		}
		counts = append(counts, TableCount{Table: spec.table, Count: count})
	}
	return counts
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

// WatermarkRow is one chat's extraction cursor joined with its name.
type WatermarkRow struct {
	ChatID        string `json:"chat_id"`
	GroupName     string `json:"group_name"`
	LastMessageID string `json:"last_message_id"`
	LastScannedAt string `json:"last_scanned_at"`
	UpdatedAt     string `json:"updated_at"`
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
		var group domain.Group
		if err := s.db.WithContext(ctx).Select("name").Where("chat_id = ?", m.ChatID).First(&group).Error; err == nil && group.Name != nil {
			row.GroupName = *group.Name
		}
		rows[i] = row
	}
	return rows, nil
}

// RecentTodos returns the newest todos as full rows so the panel can show every
// field (context_snapshot / slots / resolution) in an expandable JSON block.
func (s *DebugService) RecentTodos(ctx context.Context, limit int) ([]domain.Todo, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []domain.Todo
	if err := s.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load recent todos: %w", err)
	}
	return rows, nil
}

// RecentTasks returns the newest tasks as full rows (background / plan /
// execution_result visible via JSON expand).
func (s *DebugService) RecentTasks(ctx context.Context, limit int) ([]domain.Task, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []domain.Task
	if err := s.db.WithContext(ctx).Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load recent tasks: %w", err)
	}
	return rows, nil
}
