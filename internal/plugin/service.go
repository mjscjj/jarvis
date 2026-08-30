package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/scheduledtask"

	"gorm.io/gorm"
)

var (
	ErrNotFound              = errors.New("plugin not found")
	ErrConflict              = errors.New("plugin installation changed")
	ErrDisabled              = errors.New("plugin is disabled")
	ErrAuthorizationRequired = errors.New("plugin authorization required")
)

type UpdateInput struct {
	Enabled          bool   `json:"enabled"`
	ExpectedRevision uint64 `json:"expected_revision"`
}

type View struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Source          string     `json:"source"`
	CollectorSkill  string     `json:"collector_skill"`
	Permissions     []string   `json:"permissions"`
	IntervalMinutes int        `json:"interval_minutes"`
	Enabled         bool       `json:"enabled"`
	Revision        uint64     `json:"revision"`
	State           string     `json:"state"`
	Authorization   AuthStatus `json:"authorization"`
	ScheduledTaskID *uint64    `json:"scheduled_task_id"`
	LastRunStatus   *string    `json:"last_run_status"`
	LastError       *string    `json:"last_error"`
	LastFinishedAt  *time.Time `json:"last_finished_at"`
	NextRunAt       *time.Time `json:"next_run_at"`
	ClueCount       int64      `json:"clue_count"`
}

type Scheduler interface {
	Create(context.Context, scheduledtask.Input) (*scheduledtask.View, error)
	Get(context.Context, uint64) (*scheduledtask.View, error)
	Update(context.Context, uint64, scheduledtask.Input) (*scheduledtask.View, error)
	Trigger(context.Context, uint64) (*scheduledtask.View, error)
}

type Service struct {
	db         *gorm.DB
	registry   *Registry
	authorizer *Authorizer
	schedules  Scheduler
}

func NewService(db *gorm.DB, registry *Registry, authorizer *Authorizer, schedules Scheduler) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("plugin db is nil")
	}
	if registry == nil {
		return nil, fmt.Errorf("plugin registry is nil")
	}
	if authorizer == nil {
		return nil, fmt.Errorf("plugin authorizer is nil")
	}
	if schedules == nil {
		return nil, fmt.Errorf("plugin scheduled task service is nil")
	}
	service := &Service{db: db, registry: registry, authorizer: authorizer, schedules: schedules}
	for _, manifest := range registry.List() {
		if err := db.FirstOrCreate(&domain.PluginInstallation{PluginID: manifest.ID}, "plugin_id = ?", manifest.ID).Error; err != nil {
			return nil, fmt.Errorf("initialize plugin %s: %w", manifest.ID, err)
		}
	}
	return service, nil
}

func (s *Service) List(ctx context.Context) ([]View, error) {
	manifests := s.registry.List()
	items := make([]View, 0, len(manifests))
	for _, manifest := range manifests {
		view, err := s.view(ctx, manifest)
		if err != nil {
			return nil, err
		}
		items = append(items, *view)
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id string) (*View, error) {
	manifest, ok := s.registry.Get(id)
	if !ok {
		return nil, fmt.Errorf("%w: id=%s", ErrNotFound, id)
	}
	return s.view(ctx, manifest)
}

func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (*View, error) {
	manifest, ok := s.registry.Get(id)
	if !ok {
		return nil, fmt.Errorf("%w: id=%s", ErrNotFound, id)
	}
	result := s.db.WithContext(ctx).Model(&domain.PluginInstallation{}).
		Where("plugin_id = ? AND revision = ?", manifest.ID, input.ExpectedRevision).
		Updates(map[string]any{"enabled": input.Enabled, "revision": gorm.Expr("revision + 1")})
	if result.Error != nil {
		return nil, fmt.Errorf("update plugin %s: %w", manifest.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: id=%s", ErrConflict, manifest.ID)
	}
	authorization := s.authorizer.Probe(ctx, manifest.Provider)
	trigger := input.Enabled && authorization.Status == AuthAuthorized
	if err := s.reconcile(ctx, manifest, authorization, trigger); err != nil {
		return nil, err
	}
	return s.view(ctx, manifest)
}

func (s *Service) BeginAuthorization(ctx context.Context, id string) (*AuthStatus, error) {
	manifest, ok := s.registry.Get(id)
	if !ok {
		return nil, fmt.Errorf("%w: id=%s", ErrNotFound, id)
	}
	status := s.authorizer.Begin(ctx, manifest.Provider)
	return &status, nil
}

func (s *Service) CompleteAuthorization(ctx context.Context, id, flowID string) (*View, *AuthStatus, error) {
	manifest, ok := s.registry.Get(id)
	if !ok {
		return nil, nil, fmt.Errorf("%w: id=%s", ErrNotFound, id)
	}
	status := s.authorizer.Complete(ctx, manifest.Provider, flowID)
	if status.Status != AuthAuthorized {
		return nil, &status, nil
	}
	var installation domain.PluginInstallation
	if err := s.db.WithContext(ctx).First(&installation, "plugin_id = ?", manifest.ID).Error; err != nil {
		return nil, nil, fmt.Errorf("load plugin %s after authorization: %w", manifest.ID, err)
	}
	if installation.Enabled {
		if err := s.reconcile(ctx, manifest, status, true); err != nil {
			return nil, nil, err
		}
	}
	view, err := s.view(ctx, manifest)
	return view, &status, err
}

func (s *Service) Trigger(ctx context.Context, id string) (*View, error) {
	manifest, ok := s.registry.Get(id)
	if !ok {
		return nil, fmt.Errorf("%w: id=%s", ErrNotFound, id)
	}
	var installation domain.PluginInstallation
	if err := s.db.WithContext(ctx).First(&installation, "plugin_id = ?", manifest.ID).Error; err != nil {
		return nil, fmt.Errorf("load plugin %s: %w", manifest.ID, err)
	}
	if !installation.Enabled {
		return nil, fmt.Errorf("%w: id=%s", ErrDisabled, manifest.ID)
	}
	authorization := s.authorizer.Probe(ctx, manifest.Provider)
	if authorization.Status != AuthAuthorized {
		return nil, fmt.Errorf("%w: id=%s", ErrAuthorizationRequired, manifest.ID)
	}
	if err := s.reconcile(ctx, manifest, authorization, false); err != nil {
		return nil, err
	}
	if installation.ScheduledTaskID == nil {
		if err := s.db.WithContext(ctx).First(&installation, "plugin_id = ?", manifest.ID).Error; err != nil {
			return nil, fmt.Errorf("reload plugin %s: %w", manifest.ID, err)
		}
	}
	if installation.ScheduledTaskID == nil {
		return nil, fmt.Errorf("plugin %s schedule is unavailable", manifest.ID)
	}
	if _, err := s.schedules.Trigger(ctx, *installation.ScheduledTaskID); err != nil {
		return nil, fmt.Errorf("trigger plugin %s: %w", manifest.ID, err)
	}
	return s.view(ctx, manifest)
}

func (s *Service) SkillEnabled(ctx context.Context, name string) (bool, error) {
	pluginID, owned := s.registry.OwnerOfSkill(name)
	if !owned {
		return true, nil
	}
	var installation domain.PluginInstallation
	if err := s.db.WithContext(ctx).First(&installation, "plugin_id = ?", pluginID).Error; err != nil {
		return false, fmt.Errorf("load plugin skill owner %s: %w", pluginID, err)
	}
	return installation.Enabled, nil
}

func (s *Service) view(ctx context.Context, manifest Manifest) (*View, error) {
	var installation domain.PluginInstallation
	if err := s.db.WithContext(ctx).First(&installation, "plugin_id = ?", manifest.ID).Error; err != nil {
		return nil, fmt.Errorf("load plugin %s: %w", manifest.ID, err)
	}
	authorization := s.authorizer.Probe(ctx, manifest.Provider)
	view := &View{
		ID: manifest.ID, Name: manifest.Name, Description: manifest.Description,
		Source: manifest.Source, CollectorSkill: manifest.CollectorSkill,
		Permissions:     append([]string(nil), manifest.Permissions...),
		IntervalMinutes: manifest.IntervalMinutes, Enabled: installation.Enabled,
		Revision: installation.Revision, ScheduledTaskID: installation.ScheduledTaskID,
		Authorization: authorization, State: "disabled",
	}
	if installation.Enabled {
		if authorization.Status != AuthAuthorized {
			view.State = "needs_auth"
		} else {
			view.State = "ready"
		}
	}
	if installation.ScheduledTaskID != nil {
		schedule, err := s.schedules.Get(ctx, *installation.ScheduledTaskID)
		if err == nil {
			view.LastRunStatus = schedule.LastRunStatus
			view.LastError = schedule.LastErrorDetail
			view.LastFinishedAt = schedule.LastFinishedAt
			next := schedule.NextRunAt
			view.NextRunAt = &next
			if schedule.LastTaskID != nil {
				var task domain.Task
				if found := s.db.WithContext(ctx).First(&task, *schedule.LastTaskID); found.Error == nil {
					taskStatus := task.Status
					view.LastRunStatus = &taskStatus
					finishedAt := task.UpdatedAt
					view.LastFinishedAt = &finishedAt
					switch task.Status {
					case "pending", "executing", "awaiting_approval", "waiting", "needs_human":
						if installation.Enabled {
							view.State = "running"
						}
					case "failed":
						if installation.Enabled {
							view.State = "failed"
						}
						if task.Summary != nil && strings.TrimSpace(*task.Summary) != "" {
							summary := *task.Summary
							view.LastError = &summary
						}
					}
				}
			} else if installation.Enabled && schedule.Status == "running" {
				view.State = "running"
			} else if installation.Enabled && schedule.LastRunStatus != nil && *schedule.LastRunStatus == "failed" {
				view.State = "failed"
			}
		}
	}
	if err := s.db.WithContext(ctx).Model(&domain.Message{}).
		Where("chat_id = ?", "clue:"+manifest.Source).Count(&view.ClueCount).Error; err != nil {
		return nil, fmt.Errorf("count plugin %s clues: %w", manifest.ID, err)
	}
	return view, nil
}

func (s *Service) reconcile(ctx context.Context, manifest Manifest, authorization AuthStatus, trigger bool) error {
	var installation domain.PluginInstallation
	if err := s.db.WithContext(ctx).First(&installation, "plugin_id = ?", manifest.ID).Error; err != nil {
		return fmt.Errorf("load plugin %s for reconcile: %w", manifest.ID, err)
	}
	operational := installation.Enabled && authorization.Status == AuthAuthorized
	input := scheduleInput(manifest, operational)
	if installation.ScheduledTaskID == nil {
		if !operational {
			return nil
		}
		schedule, err := s.schedules.Create(ctx, input)
		if err != nil {
			return fmt.Errorf("create plugin %s schedule: %w", manifest.ID, err)
		}
		if err := s.db.WithContext(ctx).Model(&domain.PluginInstallation{}).
			Where("plugin_id = ?", manifest.ID).Update("scheduled_task_id", schedule.ID).Error; err != nil {
			return fmt.Errorf("link plugin %s schedule: %w", manifest.ID, err)
		}
		installation.ScheduledTaskID = &schedule.ID
	} else {
		if _, err := s.schedules.Update(ctx, *installation.ScheduledTaskID, input); err != nil {
			if !errors.Is(err, scheduledtask.ErrNotFound) {
				return fmt.Errorf("update plugin %s schedule: %w", manifest.ID, err)
			}
			if err := s.db.WithContext(ctx).Model(&domain.PluginInstallation{}).
				Where("plugin_id = ?", manifest.ID).Update("scheduled_task_id", nil).Error; err != nil {
				return fmt.Errorf("unlink missing plugin %s schedule: %w", manifest.ID, err)
			}
			return s.reconcile(ctx, manifest, authorization, trigger)
		}
	}
	if trigger && operational && installation.ScheduledTaskID != nil {
		if _, err := s.schedules.Trigger(ctx, *installation.ScheduledTaskID); err != nil {
			return fmt.Errorf("trigger plugin %s initial collection: %w", manifest.ID, err)
		}
	}
	return nil
}

func scheduleInput(manifest Manifest, enabled bool) scheduledtask.Input {
	instruction := fmt.Sprintf(
		"执行插件 %s 的采集。先读取 Skill `%s` 并严格按其步骤操作；只采集原始事实，通过 jarvis-tools append-clue 投递，不在本 Task 内判断是否值得行动。",
		manifest.Name, manifest.CollectorSkill,
	)
	interval := manifest.IntervalMinutes
	return scheduledtask.Input{
		DispatchKind: "create_task", DispatchPayload: json.RawMessage(`{}`),
		Title: "插件采集：" + manifest.Name, ActionType: "plugin_collect",
		Instruction: instruction, ContextSnapshot: json.RawMessage(`{}`),
		ScheduleType: "interval", IntervalMinutes: &interval, Enabled: &enabled,
	}
}
