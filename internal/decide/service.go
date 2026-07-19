package decide

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"

	"github.com/cloudwego/hertz/pkg/common/hlog"
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
	db  *gorm.DB
	now func() time.Time
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

func (s *Service) Approve(ctx context.Context, input ApproveInput) (*TaskView, error) {
	if err := validateCommonInput(input.TodoID, input.ExpectedVersion, input.Channel); err != nil {
		return nil, err
	}
	plan, err := canonicalJSONObject(input.Plan, "plan")
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
		slots, err := canonicalJSONObject(todo.Slots, "Todo slots")
		if err != nil {
			return err
		}
		actionHash, err := ActionHash(todo.ActionType, slots, plan)
		if err != nil {
			return err
		}
		confirmedAt := s.now().UTC()
		created = domain.Task{
			TodoID: todo.ID, Title: todo.Title, ActionType: todo.ActionType,
			Background: datatypes.JSON(append([]byte(nil), background...)),
			Plan:       datatypes.JSON(append([]byte(nil), plan...)), Slots: datatypes.JSON(slots),
			ConfirmedBy: "user", ConfirmedAt: confirmedAt, ActionHash: actionHash,
			Status: "pending", AutonomyMode: "copilot", ProjectID: copyUint64(todo.ProjectID), Version: 0,
		}
		if err := tx.Create(&created).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return fmt.Errorf("%w: todo_id=%d", ErrTaskExists, todo.ID)
			}
			return fmt.Errorf("create Task todo_id=%d: %w", todo.ID, err)
		}
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

// SupplementInput carries a human clarification for a need_info Todo.
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

// Supplement appends a human clarification to a need_info Todo's context_snapshot
// and re-queues it for M4 (status back to extracted). It then kicks an async
// re-evaluation so the decision refreshes without waiting for the cron. Writes
// are sequential and fail-fast (no transaction, per AGENTS.md).
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
	if todo.Status != "need_info" {
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

	// Re-queue: write the enriched snapshot and move need_info -> extracted with
	// optimistic locking. clearing route/confidence/risk so the re-eval starts clean.
	result := s.db.WithContext(ctx).Model(&domain.Todo{}).
		Where("id = ? AND version = ? AND status = ?", todo.ID, input.ExpectedVersion, "need_info").
		Updates(map[string]any{
			"context_snapshot": datatypes.JSON(snapshotRaw),
			"status":           "extracted",
			"route":            nil,
			"confidence":       nil,
			"risk":             nil,
			"version":          gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("apply supplement todo_id=%d: %w", todo.ID, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("%w: todo_id=%d expected_version=%d", ErrVersionConflict, todo.ID, input.ExpectedVersion)
	}
	newVersion := todo.Version + 1
	if err := s.appendSupplementEvent(ctx, todo.ID, newVersion, note, input.Channel); err != nil {
		return nil, err
	}

	s.reEvaluateAsync(todo.ID, newVersion)

	return &SupplementResult{TodoID: todo.ID, Status: "extracted", Version: newVersion}, nil
}

func (s *Service) appendSupplementEvent(ctx context.Context, todoID uint64, version int32, note, channel string) error {
	detail, err := json.Marshal(map[string]any{
		"event_type": "supplemented", "note": note, "channel": channel, "version": version,
	})
	if err != nil {
		return fmt.Errorf("encode supplement event detail todo_id=%d: %w", todoID, err)
	}
	from := "need_info"
	event := domain.TodoEvent{
		TodoID: todoID, FromStatus: &from, ToStatus: "extracted", Actor: "user", Detail: datatypes.JSON(detail),
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
func (s *Service) reEvaluateAsync(todoID uint64, expectedVersion int32) {
	if s.evaluator == nil || s.writer == nil {
		return
	}
	go func() {
		ctx := context.Background()
		var todo domain.Todo
		if err := s.db.WithContext(ctx).First(&todo, todoID).Error; err != nil {
			hlog.Errorf("supplement re-eval load todo_id=%d: %v", todoID, err)
			return
		}
		if todo.Status != "extracted" || todo.Version != expectedVersion {
			// Something else moved the Todo; leave it to the scheduled worker.
			return
		}
		evalInput, err := s.evaluator.Evaluate(ctx, &todo)
		if err != nil {
			hlog.Errorf("supplement re-eval evaluate todo_id=%d: %v", todoID, err)
			return
		}
		if _, err := s.writer.Apply(ctx, *evalInput); err != nil {
			hlog.Errorf("supplement re-eval apply todo_id=%d: %v", todoID, err)
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
	from := fromStatus
	event := domain.TodoEvent{
		TodoID: todoID, FromStatus: &from, ToStatus: toStatus, Actor: "m4", Detail: datatypes.JSON(encoded),
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("create M4 Todo event todo_id=%d: %w", todoID, err)
	}
	return nil
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

func ActionHash(actionType string, slots, plan json.RawMessage) (string, error) {
	if strings.TrimSpace(actionType) == "" {
		return "", fmt.Errorf("%w: action_type must be non-blank", ErrInvalidInput)
	}
	canonicalSlots, err := canonicalJSONObject(slots, "slots")
	if err != nil {
		return "", err
	}
	canonicalPlan, err := canonicalJSONObject(plan, "plan")
	if err != nil {
		return "", err
	}
	payload := struct {
		ActionType string          `json:"action_type"`
		Slots      json.RawMessage `json:"slots"`
		Plan       json.RawMessage `json:"plan"`
	}{ActionType: actionType, Slots: canonicalSlots, Plan: canonicalPlan}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Task action hash: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
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
		ID: task.ID, TodoID: task.TodoID, Title: task.Title, ActionType: task.ActionType,
		Background: rawJSON(task.Background), Plan: rawJSON(task.Plan), Slots: rawJSON(task.Slots),
		ConfirmedBy: task.ConfirmedBy, ConfirmedAt: task.ConfirmedAt, ActionHash: task.ActionHash,
		Status: task.Status, AutonomyMode: task.AutonomyMode, ProjectID: copyUint64(task.ProjectID), Version: task.Version,
	}
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
