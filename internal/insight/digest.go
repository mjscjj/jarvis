package insight

import (
	"context"
	"fmt"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// DigestService serves the Progress tab: a per-day timeline of "my" activity
// (leader-assigned / my todos and tasks) and key-group activity
// (messages captured, todos extracted). Pure aggregation, no cache, no cron.
type DigestService struct {
	db       *gorm.DB
	location *time.Location
}

func NewDigestService(db *gorm.DB, location *time.Location) (*DigestService, error) {
	if db == nil {
		return nil, fmt.Errorf("digest service db is nil")
	}
	if location == nil {
		return nil, fmt.Errorf("digest service location is nil")
	}
	return &DigestService{db: db, location: location}, nil
}

// MyDay is one day of the principal's progress.
type MyDay struct {
	Date         string `json:"date"`          // YYYY-MM-DD in configured timezone
	TodosCreated int64  `json:"todos_created"` // 当天新抽出的、leader 交办或与我相关的 Todo
	TasksCreated int64  `json:"tasks_created"` // 当天生成的 Task
	TasksDone    int64  `json:"tasks_done"`    // 当天完成的 Task
	TasksFailed  int64  `json:"tasks_failed"`  // 当天失败的 Task
}

// GroupDay is one day of one key group's activity.
type GroupDay struct {
	Date           string `json:"date"`
	Messages       int64  `json:"messages"`
	TodosExtracted int64  `json:"todos_extracted"`
}

// GroupProgress is a key group with its per-day activity over the window.
type GroupProgress struct {
	GroupID uint64     `json:"group_id"`
	ChatID  string     `json:"chat_id"`
	Name    string     `json:"name"`
	Days    []GroupDay `json:"days"`
}

// Digest is the whole Progress payload over the requested day window.
type Digest struct {
	Days      int             `json:"days"` // 窗口天数
	Mine      []MyDay         `json:"mine"`
	KeyGroups []GroupProgress `json:"key_groups"`
}

// dayBucket is a [start,end) time range labelled by its date string.
type dayBucket struct {
	label string
	start time.Time
	end   time.Time
}

// buildBuckets returns the last `days` calendar days (oldest→newest) in tz.
func (s *DigestService) buildBuckets(days int) []dayBucket {
	now := time.Now().In(s.location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
	buckets := make([]dayBucket, days)
	for i := 0; i < days; i++ {
		start := today.AddDate(0, 0, -(days - 1 - i))
		buckets[i] = dayBucket{
			label: start.Format("2006-01-02"),
			start: start,
			end:   start.AddDate(0, 0, 1),
		}
	}
	return buckets
}

func (s *DigestService) Load(ctx context.Context, days int) (*Digest, error) {
	if days <= 0 || days > 60 {
		return nil, fmt.Errorf("digest days must be between 1 and 60")
	}
	buckets := s.buildBuckets(days)
	windowStart := buckets[0].start

	digest := &Digest{Days: days}

	mine, err := s.loadMine(ctx, buckets)
	if err != nil {
		return nil, err
	}
	digest.Mine = mine

	keyGroups, err := s.loadKeyGroups(ctx, buckets, windowStart)
	if err != nil {
		return nil, err
	}
	digest.KeyGroups = keyGroups
	return digest, nil
}

func (s *DigestService) loadMine(ctx context.Context, buckets []dayBucket) ([]MyDay, error) {
	days := make([]MyDay, len(buckets))
	for i, bucket := range buckets {
		day := MyDay{Date: bucket.label}
		// 我相关的新 Todo：leader 交办的（M3 抽取时刻 first_seen_at 落在当天）。
		if err := s.db.WithContext(ctx).Model(&domain.Todo{}).
			Where("is_leader_assigned = ? AND first_seen_at >= ? AND first_seen_at < ?", true, bucket.start, bucket.end).
			Count(&day.TodosCreated).Error; err != nil {
			return nil, fmt.Errorf("count my todos on %s: %w", bucket.label, err)
		}
		// 当天生成 Task：使用 created 业务事件，不再从当前行猜历史。
		if err := s.db.WithContext(ctx).Model(&domain.TaskEvent{}).
			Where("event_type = ? AND occurred_at >= ? AND occurred_at < ?", "created", bucket.start, bucket.end).
			Count(&day.TasksCreated).Error; err != nil {
			return nil, fmt.Errorf("count created tasks on %s: %w", bucket.label, err)
		}
		// 当天完成/失败：直接读取状态机事件时间；重跑产生的新完成也会如实计入。
		if err := s.db.WithContext(ctx).Model(&domain.TaskEvent{}).
			Where("event_type = ? AND occurred_at >= ? AND occurred_at < ?", "execution_succeeded", bucket.start, bucket.end).
			Count(&day.TasksDone).Error; err != nil {
			return nil, fmt.Errorf("count done tasks on %s: %w", bucket.label, err)
		}
		if err := s.db.WithContext(ctx).Model(&domain.TaskEvent{}).
			Where("event_type IN ? AND occurred_at >= ? AND occurred_at < ?",
				[]string{"execution_failed", "approval_rejected", "stale_failed"}, bucket.start, bucket.end).
			Count(&day.TasksFailed).Error; err != nil {
			return nil, fmt.Errorf("count failed tasks on %s: %w", bucket.label, err)
		}
		days[i] = day
	}
	return days, nil
}

func (s *DigestService) loadKeyGroups(ctx context.Context, buckets []dayBucket, windowStart time.Time) ([]GroupProgress, error) {
	var groups []domain.Group
	if err := s.db.WithContext(ctx).
		Where("is_key_group = ?", true).
		Order("last_active_at DESC, id ASC").
		Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("load key groups: %w", err)
	}
	result := make([]GroupProgress, 0, len(groups))
	for i := range groups {
		group := &groups[i]
		progress := GroupProgress{GroupID: group.ID, ChatID: group.ChatID}
		if group.Name != nil {
			progress.Name = *group.Name
		}
		progress.Days = make([]GroupDay, len(buckets))
		for j, bucket := range buckets {
			day := GroupDay{Date: bucket.label}
			// message.create_time 是 Unix 毫秒。
			if err := s.db.WithContext(ctx).Model(&domain.Message{}).
				Where("group_id = ? AND create_time >= ? AND create_time < ?",
					group.ID, bucket.start.UnixMilli(), bucket.end.UnixMilli()).
				Count(&day.Messages).Error; err != nil {
				return nil, fmt.Errorf("count group %d messages on %s: %w", group.ID, bucket.label, err)
			}
			if err := s.db.WithContext(ctx).Model(&domain.Todo{}).
				Where("group_id = ? AND first_seen_at >= ? AND first_seen_at < ?",
					group.ID, bucket.start, bucket.end).
				Count(&day.TodosExtracted).Error; err != nil {
				return nil, fmt.Errorf("count group %d todos on %s: %w", group.ID, bucket.label, err)
			}
			progress.Days[j] = day
		}
		result = append(result, progress)
	}
	return result, nil
}
