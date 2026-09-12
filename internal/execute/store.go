// Package execute owns the MVP Task execution lifecycle.
package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
	"jarvis/internal/datatypes"
)

var (
	ErrTaskNotFound      = errors.New("execution Task not found")
	ErrRunNotFound       = errors.New("execution run not found")
	ErrVersionConflict   = errors.New("execution version conflict")
	ErrInvalidTransition = errors.New("invalid execution transition")
	ErrInvalidInput      = errors.New("invalid execution input")
)

var taskStatuses = map[string]struct{}{
	"pending": {}, "executing": {}, "waiting": {}, "needs_human": {}, "done": {}, "failed": {}, "observing": {},
}

// TaskSummaryMaxChars caps Task.summary. M5 rewrites the summary in full on
// every run and it is used in compact list results. The
// ceiling is the mechanism: without one the agent keeps appending instead of
// restating where the matter stands.
const TaskSummaryMaxChars = 1000

// validateTaskSummary carries no sentinel of its own: the caller decides whether
// an over-limit summary is a rejected input (store) or a broken agent contract
// worth a rewrite (result parser).
func validateTaskSummary(summary string) error {
	n := utf8.RuneCountInString(summary)
	if n > TaskSummaryMaxChars {
		return fmt.Errorf("summary 有 %d 字符，上限 %d。请先压缩：把已经结束的细节合并成一句结论、删掉不再影响后续判断的过程，再重新提交",
			n, TaskSummaryMaxChars)
	}
	return nil
}

type TaskFilter struct {
	Query             string
	SourceMessageID   string
	Plugin            string
	Statuses          []string
	ActionType        string
	ExcludeActionType string
	ProjectID         *uint64
	// GroupID matches through the source Todo: a Task has no group of its own,
	// and manual/scheduled/proactive Tasks legitimately belong to no group.
	GroupID *uint64
	// From / Until narrow by COALESCE(last_progress_at, created_at) as a
	// half-open RFC3339 window. Callers own the timezone.
	From     *time.Time
	Until    *time.Time
	Page     int
	PageSize int
}

type TaskList struct {
	Items    []TaskView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type TaskView struct {
	SourceURL            string                `json:"source_url,omitempty"`
	SourceMessageIDs     json.RawMessage       `json:"source_message_ids,omitempty"`
	ID                   uint64                `json:"id"`
	TodoID               *uint64               `json:"todo_id"`
	Title                string                `json:"title"`
	ActionType           string                `json:"action_type"`
	Target               string                `json:"target"`
	SourcePayload        json.RawMessage       `json:"source_payload"`
	SourceType           string                `json:"source_type"`
	SourceID             *uint64               `json:"source_id"`
	OccurrenceKey        *string               `json:"occurrence_key"`
	Status               string                `json:"status"`
	ExecutionResult      json.RawMessage       `json:"execution_result"`
	Summary              *string               `json:"summary"`
	LastProgressAt       *time.Time            `json:"last_progress_at"`
	ExecutionSupplements []ExecutionSupplement `json:"execution_supplements,omitempty"`
	ProjectID            *uint64               `json:"project_id"`
	RepoPath             *string               `json:"repo_path"`
	Version              int32                 `json:"version"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
	Resolution           *TaskResolutionView   `json:"resolution"`
}

// TaskResolutionView projects the append-only terminal TaskEvent that most
// recently resolved a Task. task_event remains the only source of truth; this
// small view lets list clients show whether a human or an Agent closed the work
// without loading every Task's full event history.
type TaskResolutionView struct {
	EventType  string    `json:"event_type"`
	ActorType  string    `json:"actor_type"`
	ActorRef   *string   `json:"actor_ref"`
	OccurredAt time.Time `json:"occurred_at"`
}

type FinishInput struct {
	TaskID          uint64
	ExpectedVersion int32
	Status          string
	Result          json.RawMessage
	ActorType       string
	ActorRef        *string
	RunID           *uint64
	EventType       string
}

type CloseInput struct {
	TaskID          uint64
	ExpectedVersion int32
	Result          json.RawMessage
	ActorType       string
	ActorRef        *string
}

// TaskUpdateInput changes the mutable, current execution surface of a Task.
// SourcePayload is deliberately absent: it is frozen source evidence and must
// never be rewritten as the Agent's understanding evolves.
type TaskUpdateInput struct {
	TaskID          uint64
	ExpectedVersion int32
	Title           *string
	Target          *string
	Summary         *string
	Instruction     *string
	Reason          string
	ActorType       string
	ActorRef        *string
}

type SupplementInput struct {
	TaskID          uint64
	ExpectedVersion int32
	Note            string
	Channel         string
}

type HumanResumeClaim struct {
	TaskID      uint64
	SourceRunID uint64
	Version     int32
	Response    string
}

// RunView 是一次 ExecutionRun 审计记录的只读视图，供任务详情展示执行历史。
// Prompt 原样返回，便于在任务详情中核对模型收到的完整输入。
type RunView struct {
	ID             uint64          `json:"id"`
	TaskID         uint64          `json:"task_id"`
	ActionType     string          `json:"action_type"`
	Stage          string          `json:"stage"`
	Sandbox        string          `json:"sandbox"`
	Status         string          `json:"status"`
	Prompt         string          `json:"prompt"`
	CodexSessionID *string         `json:"codex_session_id"`
	Summary        *string         `json:"summary"`
	Output         json.RawMessage `json:"output"`
	Effects        json.RawMessage `json:"effects"`
	ErrorDetail    *string         `json:"error_detail"`
	RepoPath       *string         `json:"repo_path"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at"`
	DurationMs     *int64          `json:"duration_ms"`
}

type RunList struct {
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
	Items    []RunView `json:"items"`
}

type TaskService interface {
	ListTasks(context.Context, TaskFilter) (*TaskList, error)
	GetTask(context.Context, uint64) (*TaskView, error)
	Finish(context.Context, FinishInput) (*TaskView, error)
	Close(context.Context, CloseInput) (*TaskView, error)
	UpdateTask(context.Context, TaskUpdateInput) (*TaskView, error)
	Supplement(context.Context, SupplementInput) (*TaskView, error)
	ListRuns(context.Context, uint64, RunFilter) (*RunList, error)
	GetRun(context.Context, uint64) (*RunView, error)
}

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("execution store db is nil")
	}
	return &Store{db: db}, nil
}

func (s *Store) Finish(ctx context.Context, input FinishInput) (*TaskView, error) {
	if input.TaskID == 0 || input.ExpectedVersion < 0 {
		return nil, fmt.Errorf("%w: Task ID/version is invalid", ErrInvalidInput)
	}
	if input.Status != "done" && input.Status != "failed" && input.Status != "observing" {
		return nil, fmt.Errorf("%w: status must be done, failed or observing", ErrInvalidInput)
	}
	result, err := canonicalJSONObject(input.Result)
	if err != nil {
		return nil, err
	}
	var finished domain.Task
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task domain.Task
		err := tx.First(&task, input.TaskID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, input.TaskID)
		}
		if err != nil {
			return fmt.Errorf("lock execution Task id=%d: %w", input.TaskID, err)
		}
		if task.Version != input.ExpectedVersion {
			return fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, input.ExpectedVersion, task.Version)
		}
		if task.Status != "pending" && task.Status != "executing" {
			return fmt.Errorf("%w: task_id=%d from=%s to=%s", ErrInvalidTransition, task.ID, task.Status, input.Status)
		}
		fromStatus := task.Status
		update := tx.Model(&domain.Task{}).
			Where("id = ? AND version = ? AND status = ?", task.ID, input.ExpectedVersion, fromStatus).
			Updates(map[string]any{
				"status": input.Status, "execution_result": datatypes.JSON(result), "version": gorm.Expr("version + 1"),
			})
		if update.Error != nil {
			return fmt.Errorf("finish execution Task id=%d: %w", task.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, input.ExpectedVersion)
		}
		if err := closeUnboundContinuations(tx, task.ID, "agent finished without a matching waiting outcome"); err != nil {
			return err
		}
		if input.Status == "observing" {
			if err := parkClueAsObserving(tx, &task); err != nil {
				return err
			}
		}
		task.Status = input.Status
		task.ExecutionResult = datatypes.JSON(result)
		task.Version++
		eventType := "execution_succeeded"
		switch input.Status {
		case "failed":
			eventType = "execution_failed"
		case "observing":
			eventType = "execution_observing"
		}
		if input.EventType != "" {
			eventType = input.EventType
		}
		if err := progress.AppendTaskEvent(tx, progress.TaskEventInput{
			TaskID: task.ID, TaskVersion: task.Version, EventType: eventType,
			FromStatus: &fromStatus, ToStatus: input.Status,
			ActorType: input.ActorType, ActorRef: input.ActorRef, RunID: input.RunID,
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		finished = task
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := taskView(ctx, &finished)
	if resolution, err := s.taskResolution(ctx, finished.ID); err != nil {
		return nil, err
	} else {
		view.Resolution = resolution
	}
	return &view, nil
}

// Close resolves verified completed, cancelled, invalidated or superseded work
// without pretending that M5 executed it. The caller supplies the complete
// semantic close result for the TaskEvent and current summary. The existing
// execution_result remains the immutable result of the last M5 execution.
func (s *Store) Close(ctx context.Context, input CloseInput) (*TaskView, error) {
	if input.TaskID == 0 || input.ExpectedVersion < 0 || strings.TrimSpace(input.ActorType) == "" {
		return nil, fmt.Errorf("%w: Task ID/version and actor type are required", ErrInvalidInput)
	}
	result, err := canonicalJSONObject(input.Result)
	if err != nil {
		return nil, err
	}
	var resultObject map[string]any
	if err := json.Unmarshal(result, &resultObject); err != nil {
		return nil, fmt.Errorf("%w: decode close result: %v", ErrInvalidInput, err)
	}
	summary, ok := resultObject["summary"].(string)
	summary = strings.TrimSpace(summary)
	if !ok || summary == "" {
		return nil, fmt.Errorf("%w: close result.summary is required", ErrInvalidInput)
	}
	if err := validateTaskSummary(summary); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	db := s.db.WithContext(ctx)
	var closed domain.Task
	err = db.First(&closed, input.TaskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, input.TaskID)
	}
	if err != nil {
		return nil, fmt.Errorf("load Task for close id=%d: %w", input.TaskID, err)
	}
	if closed.Version != input.ExpectedVersion {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, closed.ID, input.ExpectedVersion, closed.Version)
	}
	if isTerminalTaskStatus(closed.Status) {
		return nil, fmt.Errorf("%w: task_id=%d status=%s is already terminal", ErrInvalidTransition, closed.ID, closed.Status)
	}
	fromStatus := closed.Status
	occurredAt := time.Now().UTC()
	update := db.Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", closed.ID, input.ExpectedVersion, fromStatus).
		Updates(map[string]any{
			"status": "done", "summary": summary,
			"version": gorm.Expr("version + 1"), "last_progress_at": occurredAt,
		})
	if update.Error != nil {
		return nil, fmt.Errorf("close Task id=%d: %w", closed.ID, update.Error)
	}
	if update.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, closed.ID, input.ExpectedVersion)
	}
	if err := progress.AppendTaskEvent(db, progress.TaskEventInput{
		TaskID: closed.ID, TaskVersion: closed.Version + 1, EventType: "closed",
		FromStatus: &fromStatus, ToStatus: "done", ActorType: input.ActorType,
		ActorRef: input.ActorRef, Detail: json.RawMessage(result), OccurredAt: occurredAt,
	}); err != nil {
		return nil, err
	}
	// A claimed trigger rechecks the Task; future bound and unbound triggers
	// can be completed here. No semantic close decision belongs in this layer.
	cleanup := db.Model(&domain.ScheduledTask{}).
		Where("dispatch_kind = ? AND subject_type = ? AND subject_id = ? AND status IN ?",
			"resume_task", "task", closed.ID, []string{"binding", "active"}).
		Updates(map[string]any{
			"status": "completed", "last_run_status": "done",
			"last_error_detail": nil, "last_result": "Task closed: " + summary,
			"last_finished_at": occurredAt,
		})
	if cleanup.Error != nil {
		return nil, fmt.Errorf("close continuation schedules for task_id=%d: %w", closed.ID, cleanup.Error)
	}
	closed.Status = "done"
	closed.Summary = &summary
	closed.LastProgressAt = &occurredAt
	closed.Version++
	closed.UpdatedAt = occurredAt
	view := taskView(ctx, &closed)
	view.Resolution = &TaskResolutionView{
		EventType: "closed", ActorType: input.ActorType, ActorRef: input.ActorRef, OccurredAt: occurredAt,
	}
	return &view, nil
}

// UpdateTask lets a trusted Agent maintain a Task as the world changes
// instead of forcing the binary choice between leaving stale wording untouched
// and closing the work. It can update the mutable hints/current standing and
// append a future M5 instruction, while frozen source evidence remains intact.
func (s *Store) UpdateTask(ctx context.Context, input TaskUpdateInput) (*TaskView, error) {
	if input.TaskID == 0 || input.ExpectedVersion < 0 || strings.TrimSpace(input.ActorType) == "" {
		return nil, fmt.Errorf("%w: Task ID/version and actor type are required", ErrInvalidInput)
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: update reason is required", ErrInvalidInput)
	}
	if input.Title == nil && input.Target == nil && input.Summary == nil && input.Instruction == nil {
		return nil, fmt.Errorf("%w: at least one Task field must be updated", ErrInvalidInput)
	}

	var task domain.Task
	if err := s.db.WithContext(ctx).First(&task, input.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, input.TaskID)
		}
		return nil, fmt.Errorf("load Task for update id=%d: %w", input.TaskID, err)
	}
	if task.Version != input.ExpectedVersion {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, input.ExpectedVersion, task.Version)
	}
	if isTerminalTaskStatus(task.Status) || task.Status == "executing" {
		return nil, fmt.Errorf("%w: task_id=%d status=%s cannot be updated", ErrInvalidTransition, task.ID, task.Status)
	}
	// A needs_human Task is showing the principal a question built from its
	// current goal. Rewriting that goal behind the card would make the answer
	// mean something else; only the visible standing may be refreshed.
	if task.Status == "needs_human" && (input.Title != nil || input.Target != nil || input.Instruction != nil) {
		return nil, fmt.Errorf("%w: task_id=%d needs_human only permits summary updates", ErrInvalidTransition, task.ID)
	}

	updates := map[string]any{}
	changes := map[string]any{}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: title must be non-blank", ErrInvalidInput)
		}
		if title != task.Title {
			updates["title"] = title
			changes["title"] = title
		}
	}
	if input.Target != nil {
		target := strings.TrimSpace(*input.Target)
		if target != task.Target {
			updates["target"] = target
			changes["target"] = target
		}
	}
	if input.Summary != nil {
		summary := strings.TrimSpace(*input.Summary)
		if summary == "" {
			return nil, fmt.Errorf("%w: summary must be non-blank", ErrInvalidInput)
		}
		if err := validateTaskSummary(summary); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
		if task.Summary == nil || strings.TrimSpace(*task.Summary) != summary {
			updates["summary"] = summary
			updates["last_progress_at"] = time.Now().UTC()
			changes["summary"] = summary
		}
	}
	if input.Instruction != nil {
		instruction := strings.TrimSpace(*input.Instruction)
		if instruction == "" {
			return nil, fmt.Errorf("%w: instruction must be non-blank", ErrInvalidInput)
		}
		encoded, err := appendExecutionSupplement(task.ExecutionSupplements, instruction, "agent:"+input.ActorType, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("append Agent instruction task_id=%d: %w", task.ID, err)
		}
		updates["execution_supplements"] = datatypes.JSON(encoded)
		changes["instruction"] = instruction
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("%w: update does not change the Task", ErrInvalidInput)
	}
	updates["version"] = gorm.Expr("version + 1")
	result := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", task.ID, input.ExpectedVersion, task.Status).
		Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("update Task id=%d: %w", task.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, input.ExpectedVersion)
	}

	var reloaded domain.Task
	if err := s.db.WithContext(ctx).First(&reloaded, task.ID).Error; err != nil {
		return nil, fmt.Errorf("reload Task id=%d after update: %w", task.ID, err)
	}
	if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
		TaskID: reloaded.ID, TaskVersion: reloaded.Version, EventType: "updated",
		FromStatus: &reloaded.Status, ToStatus: reloaded.Status, ActorType: input.ActorType,
		ActorRef: input.ActorRef, Detail: map[string]any{"reason": reason, "changes": changes},
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	view := taskView(ctx, &reloaded)
	return &view, nil
}

// RecordProgress stores where the matter now stands, as M5 described it at the
// end of a run.
//
// It sits outside the status-transition methods and deliberately does not bump
// version: version is the optimistic-lock token those transitions race on, so
// bumping it for a summary would make progress writes collide with a concurrent
// claim. Progress is an observation about the work, not a state change to it.
//
// last_progress_at moves only when the summary actually changes. A Task that
// keeps resuming and re-reporting the same standing is not making progress, and
// treating it as such would hide exactly the stalled work this field exists to
// surface. A blank summary is therefore a no-op, not an erasure.
func (s *Store) RecordProgress(ctx context.Context, taskID uint64, summary string, now time.Time) error {
	if taskID == 0 {
		return fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}
	// The result parser already rejected an over-limit progress_summary and gave
	// the session a chance to compact it, so reaching here means the summary
	// arrived by some other route. Reject rather than truncate: the Task keeps a
	// readable previous standing, whereas a sentence cut in half is unusable.
	if err := validateTaskSummary(summary); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	var task domain.Task
	if err := s.db.WithContext(ctx).Select("id", "summary").First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return fmt.Errorf("load execution Task id=%d for progress: %w", taskID, err)
	}
	if task.Summary != nil && strings.TrimSpace(*task.Summary) == summary {
		return nil
	}
	at := now.UTC()
	if err := s.db.WithContext(ctx).Model(&domain.Task{}).Where("id = ?", taskID).
		Updates(map[string]any{"summary": summary, "last_progress_at": at}).Error; err != nil {
		return fmt.Errorf("record progress task_id=%d: %w", taskID, err)
	}
	return nil
}

var supplementableTaskStatuses = map[string]struct{}{
	"pending": {}, "executing": {}, "waiting": {}, "needs_human": {}, "done": {}, "failed": {}, "observing": {},
}

// Supplement appends a human clarification/instruction to a Task's M5-only
// execution_supplements. It does not touch Todo.content or the Task's
// frozen source_payload evidence.
func (s *Store) Supplement(ctx context.Context, input SupplementInput) (*TaskView, error) {
	if input.TaskID == 0 || input.ExpectedVersion < 0 {
		return nil, fmt.Errorf("%w: Task ID/version is invalid", ErrInvalidInput)
	}
	note := strings.TrimSpace(input.Note)
	if note == "" {
		return nil, fmt.Errorf("%w: supplement note must be non-blank", ErrInvalidInput)
	}
	channel := strings.TrimSpace(input.Channel)
	if channel == "" {
		channel = "backend"
	}

	var task domain.Task
	if err := s.db.WithContext(ctx).First(&task, input.TaskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, input.TaskID)
		}
		return nil, fmt.Errorf("load execution Task id=%d: %w", input.TaskID, err)
	}
	if task.Version != input.ExpectedVersion {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d actual=%d", ErrVersionConflict, task.ID, input.ExpectedVersion, task.Version)
	}
	if _, ok := supplementableTaskStatuses[task.Status]; !ok {
		return nil, fmt.Errorf("%w: task_id=%d status=%s cannot be supplemented", ErrInvalidTransition, task.ID, task.Status)
	}

	encoded, err := appendExecutionSupplement(task.ExecutionSupplements, note, channel, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("append execution_supplements task_id=%d: %w", task.ID, err)
	}

	result := s.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id = ? AND version = ? AND status = ?", task.ID, input.ExpectedVersion, task.Status).
		Updates(map[string]any{
			"execution_supplements": datatypes.JSON(encoded),
			"version":               gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("apply supplement task_id=%d: %w", task.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, input.ExpectedVersion)
	}

	var reloaded domain.Task
	if err := s.db.WithContext(ctx).First(&reloaded, task.ID).Error; err != nil {
		return nil, fmt.Errorf("reload execution Task id=%d after supplement: %w", task.ID, err)
	}
	if err := progress.AppendTaskEvent(s.db.WithContext(ctx), progress.TaskEventInput{
		TaskID: reloaded.ID, TaskVersion: reloaded.Version, EventType: "supplemented",
		FromStatus: &reloaded.Status, ToStatus: reloaded.Status, ActorType: "user",
		Detail: map[string]any{"channel": channel}, OccurredAt: time.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	view := taskView(ctx, &reloaded)
	return &view, nil
}

func canonicalJSONObject(raw []byte) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: result is required", ErrInvalidInput)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("%w: decode result: %v", ErrInvalidInput, err)
	}
	if len(object) == 0 {
		return nil, fmt.Errorf("%w: result must be a non-empty JSON object", ErrInvalidInput)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("%w: result must contain one JSON object", ErrInvalidInput)
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("encode execution result: %w", err)
	}
	return encoded, nil
}

func isTerminalTaskStatus(status string) bool {
	switch status {
	case "done", "failed", "observing":
		return true
	default:
		return false
	}
}
