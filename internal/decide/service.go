package decide

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"
	"jarvis/internal/observability"
	"jarvis/internal/taskcreate"

	"code.byted.org/middleware/hertz/pkg/common/hlog"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// requireContextSnapshot returns the Todo's M3-frozen context_snapshot as raw
// JSON, failing fast if it is missing or malformed. There is deliberately no
// re-snapshot fallback: an empty snapshot is a real bug that must surface.
func requireContextSnapshot(todo *domain.Todo) (json.RawMessage, error) {
	raw := []byte(todo.ContextSnapshot)
	if _, err := contextsnap.Decode(raw); err != nil {
		return nil, fmt.Errorf("%w: todo_id=%d context_snapshot invalid: %v", ErrInvalidInput, todo.ID, err)
	}
	return json.RawMessage(append([]byte(nil), raw...)), nil
}

type Service struct {
	db       *gorm.DB
	now      func() time.Time
	notifier LifecycleNotifier
	// evaluator/writer re-run the M4 decision after a need_info supplement. They
	// are optional: when nil, Supplement only re-queues the Todo (extracted) and
	// the scheduled M4 worker picks it up. When set, Supplement kicks a re-eval
	// asynchronously so the page can refresh into the new route without waiting.
	evaluator todoEvaluator
	writer    evaluationWriter
}

func NewService(db *gorm.DB, evaluator todoEvaluator, writer evaluationWriter) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("confirmation service db is nil")
	}
	return &Service{db: db, now: time.Now, evaluator: evaluator, writer: writer}, nil
}

// SetLifecycleNotifier wires the real-time pipeline before the server starts.
// Replacing it at runtime is rejected to keep transition behavior deterministic.
func (s *Service) SetLifecycleNotifier(notifier LifecycleNotifier) error {
	if notifier == nil {
		return fmt.Errorf("confirmation lifecycle notifier is nil")
	}
	if s.notifier != nil {
		return fmt.Errorf("confirmation lifecycle notifier is already set")
	}
	s.notifier = notifier
	return nil
}

func (s *Service) Approve(ctx context.Context, input ApproveInput) (*TaskView, error) {
	if err := validateCommonInput(input.TodoID, input.ExpectedVersion, input.Channel); err != nil {
		return nil, err
	}
	plan, err := canonicalJSONValue(input.Plan, "plan", false)
	if err != nil {
		return nil, err
	}
	var initial domain.Todo
	if err := s.loadTodo(ctx, input.TodoID, &initial); err != nil {
		return nil, err
	}
	if initial.Version != input.ExpectedVersion {
		return nil, versionConflict(input.TodoID, input.ExpectedVersion, initial.Version)
	}
	if initial.Status != "need_decision" {
		return nil, transitionError(input.TodoID, initial.Status, "confirmed")
	}
	// The background is the context_snapshot M3 froze onto the Todo. M4 reuses it
	// verbatim; it must be present (fail-fast, no re-snapshot fallback — see
	// docs/design-context-pipeline.md §2.2/§2.3).
	background, err := requireContextSnapshot(&initial)
	if err != nil {
		return nil, err
	}

	var created domain.Task
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var todo domain.Todo
		if err := lockTodo(tx, input.TodoID, &todo); err != nil {
			return err
		}
		if todo.Version != input.ExpectedVersion {
			return versionConflict(todo.ID, input.ExpectedVersion, todo.Version)
		}
		if todo.Status != "need_decision" {
			return transitionError(todo.ID, todo.Status, "confirmed")
		}
		var existing domain.Task
		result := tx.Where("todo_id = ?", todo.ID).Limit(1).Find(&existing)
		if result.Error != nil {
			return fmt.Errorf("check existing Task todo_id=%d: %w", todo.ID, result.Error)
		}
		if result.RowsAffected != 0 {
			return fmt.Errorf("%w: todo_id=%d task_id=%d", ErrTaskExists, todo.ID, existing.ID)
		}
		confirmedAt := s.now().UTC()
		decisionPayload, err := loadLatestDecisionPayload(tx, todo.ID)
		if err != nil {
			return err
		}
		factory, err := taskcreate.NewFactory(tx)
		if err != nil {
			return err
		}
		todoID := todo.ID
		createdTask, err := factory.CreateWithDB(ctx, tx, taskcreate.Input{
			TodoID: &todoID, Title: todo.Title, ActionType: todo.ActionType, Target: todo.Target,
			Background: background, SourceClue: json.RawMessage(todo.ExtractionResult),
			Plan: plan, DecisionPayload: decisionPayload,
			ConfirmedBy: "user", ConfirmedAt: &confirmedAt,
			ProjectID: copyUint64(todo.ProjectID), SourceType: taskcreate.SourceTodo, SourceID: &todoID,
			ExecutionMode: taskcreate.ExecutionModeStandard, ActorType: "user",
			EventDetail: map[string]any{"channel": input.Channel},
		})
		if errors.Is(err, taskcreate.ErrExists) {
			return fmt.Errorf("%w: todo_id=%d", ErrTaskExists, todo.ID)
		}
		if err != nil {
			return err
		}
		created = *createdTask
		if err := updateTodoStatus(tx, &todo, input.ExpectedVersion, "confirmed"); err != nil {
			return err
		}
		if err := createTodoEvent(tx, todo.ID, todo.Status, "confirmed", map[string]any{
			"event_type": "approved", "task_id": created.ID, "channel": input.Channel,
		}); err != nil {
			return err
		}
		if err := tx.Create(manualAudit(&todo, &created, "user_approved", input.Channel, confirmedAt)).Error; err != nil {
			return fmt.Errorf("create approval audit todo_id=%d: %w", todo.ID, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	view := taskView(&created)
	if s.notifier != nil {
		if err := s.notifier.TaskReady(ctx, created.ID, created.Version); err != nil && !errors.Is(err, ErrLifecycleStageDisabled) {
			hlog.CtxErrorf(ctx, "notify approved task ready failed task_id=%d version=%d error=%+v", created.ID, created.Version, err)
		}
	}
	return &view, nil
}

func (s *Service) Reject(ctx context.Context, input RejectInput) (*RejectResult, error) {
	if err := validateCommonInput(input.TodoID, input.ExpectedVersion, input.Channel); err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reject reason must be non-blank", ErrInvalidInput)
	}
	result := RejectResult{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var todo domain.Todo
		if err := lockTodo(tx, input.TodoID, &todo); err != nil {
			return err
		}
		if todo.Version != input.ExpectedVersion {
			return versionConflict(todo.ID, input.ExpectedVersion, todo.Version)
		}
		if todo.Status != "need_info" && todo.Status != "need_decision" {
			return transitionError(todo.ID, todo.Status, "dismissed")
		}
		fromStatus := todo.Status
		if err := updateTodoStatus(tx, &todo, input.ExpectedVersion, "dismissed"); err != nil {
			return err
		}
		if err := createTodoEvent(tx, todo.ID, fromStatus, "dismissed", map[string]any{
			"event_type": "rejected", "reason": reason, "channel": input.Channel,
		}); err != nil {
			return err
		}
		audit := manualAudit(&todo, nil, "user_rejected", input.Channel, s.now().UTC())
		audit.FinalStatus = "dismissed"
		if err := tx.Create(audit).Error; err != nil {
			return fmt.Errorf("create rejection audit todo_id=%d: %w", todo.ID, err)
		}
		result = RejectResult{TodoID: todo.ID, Status: "dismissed", Version: todo.Version + 1}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// SupplementInput carries a human clarification for a Todo awaiting a decision
// (need_info or need_decision).
type SupplementInput struct {
	TodoID          uint64
	ExpectedVersion int32
	Note            string
	Channel         string
}

// SupplementResult reports the Todo state right after the supplement was stored.
// Status is "extracted": the Todo has been re-queued for M4. Re-evaluation runs
// asynchronously, so the caller polls the confirmation detail for the new route.
type SupplementResult struct {
	TodoID  uint64 `json:"todo_id"`
	Status  string `json:"status"`
	Version int32  `json:"version"`
}

// Supplement appends a human clarification to a Todo's context_snapshot and
// re-queues it for M4 (status back to extracted). It accepts both need_info and
// need_decision Todos, so the reviewer can steer the decision from either page.
// It then kicks an async re-evaluation so the decision refreshes without waiting
// for the cron. Writes are sequential and fail-fast (no transaction, per
// AGENTS.md).
func (s *Service) Supplement(ctx context.Context, input SupplementInput) (*SupplementResult, error) {
	if err := validateCommonInput(input.TodoID, input.ExpectedVersion, input.Channel); err != nil {
		return nil, err
	}
	note := strings.TrimSpace(input.Note)
	if note == "" {
		return nil, fmt.Errorf("%w: supplement note must be non-blank", ErrInvalidInput)
	}

	var todo domain.Todo
	if err := s.loadTodo(ctx, input.TodoID, &todo); err != nil {
		return nil, err
	}
	if todo.Version != input.ExpectedVersion {
		return nil, versionConflict(input.TodoID, input.ExpectedVersion, todo.Version)
	}
	fromStatus := todo.Status
	if fromStatus != "need_info" && fromStatus != "need_decision" {
		return nil, transitionError(input.TodoID, todo.Status, "extracted")
	}

	snapshot, err := contextsnap.Decode([]byte(todo.ContextSnapshot))
	if err != nil {
		return nil, fmt.Errorf("%w: todo_id=%d context_snapshot invalid: %v", ErrInvalidInput, todo.ID, err)
	}
	snapshot.Supplements = append(snapshot.Supplements, contextsnap.Supplement{
		Note: note, At: s.now().UTC().Format(time.RFC3339),
	})
	snapshotRaw, err := snapshot.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode supplemented context_snapshot todo_id=%d: %w", todo.ID, err)
	}

	// Re-queue: write the enriched snapshot and move need_info/need_decision ->
	// extracted with optimistic locking, clearing route/confidence/risk so the
	// re-eval starts clean.
	updates := map[string]any{
		"context_snapshot": datatypes.JSON(snapshotRaw),
		"status":           "extracted",
		"route":            nil,
		"confidence":       nil,
		"risk":             nil,
		"version":          gorm.Expr("version + 1"),
	}
	if fromStatus == RouteNeedDecision {
		// This also protects pre-migration rows whose new column defaulted to false.
		updates["manual_gate_required"] = true
	}
	result := s.db.WithContext(ctx).Model(&domain.Todo{}).
		Where("id = ? AND version = ? AND status = ?", todo.ID, input.ExpectedVersion, fromStatus).
		Updates(updates)
	if result.Error != nil {
		return nil, fmt.Errorf("apply supplement todo_id=%d: %w", todo.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: todo_id=%d expected_version=%d", ErrVersionConflict, todo.ID, input.ExpectedVersion)
	}
	newVersion := todo.Version + 1
	if err := s.appendSupplementEvent(ctx, todo.ID, newVersion, fromStatus, note, input.Channel); err != nil {
		return nil, err
	}

	s.reEvaluateAsync(ctx, todo.ID, newVersion)

	return &SupplementResult{TodoID: todo.ID, Status: "extracted", Version: newVersion}, nil
}

func (s *Service) appendSupplementEvent(ctx context.Context, todoID uint64, version int32, fromStatus, note, channel string) error {
	detail, err := json.Marshal(map[string]any{
		"event_type": "supplemented", "note": note, "channel": channel, "version": version,
	})
	if err != nil {
		return fmt.Errorf("encode supplement event detail todo_id=%d: %w", todoID, err)
	}
	snapshot, err := loadTodoEventSnapshot(s.db.WithContext(ctx), todoID)
	if err != nil {
		return err
	}
	from := fromStatus
	event := domain.TodoEvent{
		TodoID: todoID, FromStatus: &from, ToStatus: "extracted",
		Actor: "user", Detail: datatypes.JSON(detail), Snapshot: snapshot,
	}
	if err := s.db.WithContext(ctx).Create(&event).Error; err != nil {
		return fmt.Errorf("create supplement event todo_id=%d: %w", todoID, err)
	}
	return nil
}

// reEvaluateAsync re-runs M4 for one Todo in the background. It uses a fresh
// context (the request context is done once the handler returns) and swallows
// errors to logs: a failed async re-eval leaves the Todo in extracted, which the
// scheduled M4 worker will retry.
func (s *Service) reEvaluateAsync(parent context.Context, todoID uint64, expectedVersion int32) {
	detached := observability.Detached(parent)
	go func() {
		ctx := detached
		if s.notifier != nil {
			err := s.notifier.TodoReady(ctx, todoID, expectedVersion)
			switch {
			case err == nil:
				return
			case !errors.Is(err, ErrLifecycleStageDisabled):
				hlog.CtxErrorf(ctx, "notify supplemented todo ready failed todo_id=%d version=%d error=%+v", todoID, expectedVersion, err)
				return
			}
		}
		if s.evaluator == nil || s.writer == nil {
			return
		}
		var todo domain.Todo
		if err := s.db.WithContext(ctx).First(&todo, todoID).Error; err != nil {
			hlog.CtxErrorf(ctx, "supplement re-eval load failed todo_id=%d error=%+v", todoID, err)
			return
		}
		if todo.Status != "extracted" || todo.Version != expectedVersion {
			// Something else moved the Todo; leave it to the scheduled worker.
			return
		}
		evalInput, err := s.evaluator.Evaluate(ctx, &todo)
		if err != nil {
			hlog.CtxErrorf(ctx, "supplement re-eval evaluate failed todo_id=%d error=%+v", todoID, err)
			return
		}
		if evalInput == nil {
			hlog.CtxErrorf(ctx, "supplement re-eval evaluate failed todo_id=%d error=nil_result", todoID)
			return
		}
		result, err := s.writer.Apply(ctx, *evalInput)
		if err != nil {
			hlog.CtxErrorf(ctx, "supplement re-eval apply failed todo_id=%d error=%+v", todoID, err)
			return
		}
		if result == nil {
			hlog.CtxErrorf(ctx, "supplement re-eval apply failed todo_id=%d error=nil_result", todoID)
			return
		}
		if result.TaskID != nil && s.notifier != nil {
			if err := s.notifier.TaskReady(ctx, *result.TaskID, result.TaskVersion); err != nil && !errors.Is(err, ErrLifecycleStageDisabled) {
				hlog.CtxErrorf(ctx, "notify supplemented task ready failed task_id=%d version=%d error=%+v", *result.TaskID, result.TaskVersion, err)
			}
		}
	}()
}

func (s *Service) loadTodo(ctx context.Context, todoID uint64, todo *domain.Todo) error {
	err := s.db.WithContext(ctx).First(todo, todoID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: todo_id=%d", ErrTodoNotFound, todoID)
	}
	if err != nil {
		return fmt.Errorf("load confirmation Todo id=%d: %w", todoID, err)
	}
	return nil
}

func lockTodo(tx *gorm.DB, todoID uint64, todo *domain.Todo) error {
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(todo, todoID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: todo_id=%d", ErrTodoNotFound, todoID)
	}
	if err != nil {
		return fmt.Errorf("lock confirmation Todo id=%d: %w", todoID, err)
	}
	return nil
}

func updateTodoStatus(tx *gorm.DB, todo *domain.Todo, expectedVersion int32, target string) error {
	result := tx.Model(&domain.Todo{}).
		Where("id = ? AND version = ? AND status = ?", todo.ID, expectedVersion, todo.Status).
		Updates(map[string]any{"status": target, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return fmt.Errorf("update Todo status id=%d: %w", todo.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: todo_id=%d expected_version=%d", ErrVersionConflict, todo.ID, expectedVersion)
	}
	return nil
}

func createTodoEvent(tx *gorm.DB, todoID uint64, fromStatus, toStatus string, detail map[string]any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode M4 Todo event detail: %w", err)
	}
	snapshot, err := loadTodoEventSnapshot(tx, todoID)
	if err != nil {
		return err
	}
	from := fromStatus
	event := domain.TodoEvent{
		TodoID: todoID, FromStatus: &from, ToStatus: toStatus,
		Actor: "m4", Detail: datatypes.JSON(encoded), Snapshot: snapshot,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("create M4 Todo event todo_id=%d: %w", todoID, err)
	}
	return nil
}

func loadTodoEventSnapshot(db *gorm.DB, todoID uint64) (datatypes.JSON, error) {
	var todo domain.Todo
	if err := db.First(&todo, todoID).Error; err != nil {
		return nil, fmt.Errorf("load todo id=%d for event snapshot: %w", todoID, err)
	}
	snapshot, err := domain.EncodeTodoEventSnapshot(&todo)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func loadLatestDecisionPayload(db *gorm.DB, todoID uint64) (json.RawMessage, error) {
	var rows []domain.TodoEvent
	if err := db.Where("todo_id = ?", todoID).Order("id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load latest decision payload todo_id=%d: %w", todoID, err)
	}
	for _, row := range rows {
		if len(row.Detail) == 0 {
			continue
		}
		var detail struct {
			EventType string          `json:"event_type"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(row.Detail, &detail); err != nil {
			return nil, fmt.Errorf("decode decision event detail event_id=%d: %w", row.ID, err)
		}
		if detail.EventType != "evaluated" {
			continue
		}
		if len(detail.Payload) == 0 || bytes.Equal(bytes.TrimSpace(detail.Payload), []byte("null")) {
			return nil, nil
		}
		payload, err := canonicalJSONValue(detail.Payload, "decision payload", false)
		if err != nil {
			return nil, fmt.Errorf("invalid decision payload event_id=%d: %w", row.ID, err)
		}
		return payload, nil
	}
	return nil, nil
}

func manualAudit(todo *domain.Todo, task *domain.Task, reason, channel string, at time.Time) *domain.DecisionAudit {
	route := todo.Status
	if todo.Route != nil && strings.TrimSpace(*todo.Route) != "" {
		route = *todo.Route
	}
	matchedRules := datatypes.JSON([]byte("[]"))
	audit := &domain.DecisionAudit{
		TodoID: todo.ID, TS: at, Route: route, RouteReason: reason,
		ConfidenceEff: todo.Confidence, RiskEff: todo.Risk, MatchedRules: matchedRules,
		DecisionEngine: "manual", ThresholdConfigVersion: "manual-v1",
		Approver: "user", Channel: channel, FinalStatus: "confirmed",
	}
	if task != nil {
		audit.TaskID = &task.ID
		actionHash := task.ActionHash
		audit.ActionHash = &actionHash
	}
	return audit
}

// ActionHash identifies a confirmed action by (action_type, target, plan). The
// target is the clue's dedup identity from M3; together with M4's stored
// direction it fingerprints the handoff without the old per-type slot vocabulary.
func ActionHash(actionType, target string, plan json.RawMessage) (string, error) {
	hash, err := taskcreate.ActionHash(actionType, target, plan)
	if errors.Is(err, taskcreate.ErrInvalidInput) {
		return "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return hash, err
}

func canonicalJSONObject(raw []byte, name string) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: %s is required", ErrInvalidInput, name)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %v", ErrInvalidInput, name, err)
	}
	if object == nil || len(object) == 0 {
		return nil, fmt.Errorf("%w: %s must be a non-empty JSON object", ErrInvalidInput, name)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: %s contains multiple JSON values", ErrInvalidInput, name)
		}
		return nil, fmt.Errorf("%w: decode trailing %s: %v", ErrInvalidInput, name, err)
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("encode canonical %s: %w", name, err)
	}
	return json.RawMessage(encoded), nil
}

// canonicalJSONValue validates and canonicalizes one open semantic JSON value.
// It intentionally does not prescribe object fields. When allowEmpty is false,
// null, blank strings, empty arrays and empty objects are rejected.
func canonicalJSONValue(raw []byte, name string, allowEmpty bool) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: %s is required", ErrInvalidInput, name)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %v", ErrInvalidInput, name, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: %s contains multiple JSON values", ErrInvalidInput, name)
		}
		return nil, fmt.Errorf("%w: decode trailing %s: %v", ErrInvalidInput, name, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%w: %s must not be null", ErrInvalidInput, name)
	}
	if !allowEmpty {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				return nil, fmt.Errorf("%w: %s must not be blank", ErrInvalidInput, name)
			}
		case []any:
			if len(typed) == 0 {
				return nil, fmt.Errorf("%w: %s must not be an empty array", ErrInvalidInput, name)
			}
		case map[string]any:
			if len(typed) == 0 {
				return nil, fmt.Errorf("%w: %s must not be an empty object", ErrInvalidInput, name)
			}
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical %s: %w", name, err)
	}
	return json.RawMessage(encoded), nil
}

func validateCommonInput(todoID uint64, expectedVersion int32, channel string) error {
	if todoID == 0 {
		return fmt.Errorf("%w: todo_id must be positive", ErrInvalidInput)
	}
	if expectedVersion < 0 {
		return fmt.Errorf("%w: expected_version must not be negative", ErrInvalidInput)
	}
	switch channel {
	case "backend", "feishu":
	default:
		return fmt.Errorf("%w: unsupported confirmation channel %q", ErrInvalidInput, channel)
	}
	return nil
}

func versionConflict(todoID uint64, expected, actual int32) error {
	return fmt.Errorf("%w: todo_id=%d expected=%d actual=%d", ErrVersionConflict, todoID, expected, actual)
}

func transitionError(todoID uint64, from, to string) error {
	return fmt.Errorf("%w: todo_id=%d from=%s to=%s", ErrInvalidTransition, todoID, from, to)
}

func taskView(task *domain.Task) TaskView {
	return TaskView{
		ID: task.ID, TodoID: derefUint64(task.TodoID), Title: task.Title, ActionType: task.ActionType, Target: task.Target,
		Background: rawJSON(task.Background), Plan: rawJSON(task.Plan), DecisionPayload: rawJSON(task.DecisionPayload),
		ConfirmedBy: task.ConfirmedBy, ConfirmedAt: task.ConfirmedAt, ActionHash: task.ActionHash,
		SourceType: task.SourceType, SourceID: copyUint64(task.SourceID), ExecutionMode: task.ExecutionMode,
		Status: task.Status, AutonomyMode: task.AutonomyMode, ProjectID: copyUint64(task.ProjectID), Version: task.Version,
	}
}

func derefUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
