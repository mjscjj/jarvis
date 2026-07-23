package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/taskcreate"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type EvaluationInput struct {
	TodoID                 uint64
	ExpectedVersion        int32
	Confidence             float64
	Risk                   float64
	Route                  string
	RouteReason            string
	ConfidenceFactors      []DecisionFactor
	RiskFactors            []DecisionFactor
	MatchedRules           []string
	DecisionEngine         string
	CodexSessionID         *string
	PromptVersion          string
	ThresholdConfigVersion string
	FailureDetail          string
	ProposedPlan           *PlanDraft
	Clarifications         []Clarification
	EvidenceGathered       []Evidence
	ManualGate             bool
}

type EvaluationResult struct {
	TodoID      uint64  `json:"todo_id"`
	Status      string  `json:"status"`
	Version     int32   `json:"version"`
	Confidence  float64 `json:"confidence"`
	Risk        float64 `json:"risk"`
	TaskID      *uint64 `json:"task_id,omitempty"`
	TaskVersion int32   `json:"task_version,omitempty"`
}

type EvaluationStore struct {
	db  *gorm.DB
	now func() time.Time
}

func NewEvaluationStore(db *gorm.DB) (*EvaluationStore, error) {
	if db == nil {
		return nil, fmt.Errorf("evaluation store db is nil")
	}
	return &EvaluationStore{db: db, now: time.Now}, nil
}

func (s *EvaluationStore) Apply(ctx context.Context, input EvaluationInput) (*EvaluationResult, error) {
	if err := validateEvaluationInput(input); err != nil {
		return nil, err
	}
	confidenceFactors, err := json.Marshal(input.ConfidenceFactors)
	if err != nil {
		return nil, fmt.Errorf("encode confidence factors: %w", err)
	}
	riskFactors, err := json.Marshal(input.RiskFactors)
	if err != nil {
		return nil, fmt.Errorf("encode risk factors: %w", err)
	}
	matchedRules, err := json.Marshal(input.MatchedRules)
	if err != nil {
		return nil, fmt.Errorf("encode matched rules: %w", err)
	}
	var result EvaluationResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var todo domain.Todo
		if err := lockTodo(tx, input.TodoID, &todo); err != nil {
			return err
		}
		if todo.Version != input.ExpectedVersion {
			return versionConflict(todo.ID, input.ExpectedVersion, todo.Version)
		}
		if todo.Status != "extracted" {
			return transitionError(todo.ID, todo.Status, input.Route)
		}
		updates := map[string]any{
			"route": input.Route, "status": input.Route, "version": gorm.Expr("version + 1"),
		}
		if input.Route == RouteNeedDecision {
			updates["manual_gate_required"] = true
		}
		if !input.ManualGate {
			updates["confidence"] = input.Confidence
			updates["risk"] = input.Risk
		}
		update := tx.Model(&domain.Todo{}).
			Where("id = ? AND version = ? AND status = ?", todo.ID, input.ExpectedVersion, "extracted").
			Updates(updates)
		if update.Error != nil {
			return fmt.Errorf("apply Todo evaluation id=%d: %w", todo.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: todo_id=%d expected_version=%d", ErrVersionConflict, todo.ID, input.ExpectedVersion)
		}
		eventDetail := map[string]any{
			"event_type": "evaluated", "route_reason": input.RouteReason,
			"matched_rules": input.MatchedRules, "decision_engine": input.DecisionEngine,
			"prompt_version": input.PromptVersion, "failure_detail": input.FailureDetail,
			"proposed_plan":     input.ProposedPlan,
			"clarifications":    input.Clarifications,
			"evidence_gathered": input.EvidenceGathered,
		}
		if !input.ManualGate {
			eventDetail["confidence"] = input.Confidence
			eventDetail["risk"] = input.Risk
			eventDetail["confidence_factors"] = input.ConfidenceFactors
			eventDetail["risk_factors"] = input.RiskFactors
		}
		if err := createTodoEvent(tx, todo.ID, "extracted", input.Route, eventDetail); err != nil {
			return err
		}
		audit := domain.DecisionAudit{
			TodoID: todo.ID, TS: s.now().UTC(), Route: input.Route, RouteReason: input.RouteReason,
			ConfidenceFactors: datatypes.JSON(confidenceFactors), RiskFactors: datatypes.JSON(riskFactors), MatchedRules: datatypes.JSON(matchedRules),
			DecisionEngine: input.DecisionEngine, CodexSessionID: copyString(input.CodexSessionID),
			ThresholdConfigVersion: input.ThresholdConfigVersion, Channel: "auto", FinalStatus: input.Route,
		}
		if !input.ManualGate {
			audit.ConfidenceEff = float64Pointer(input.Confidence)
			audit.RiskEff = float64Pointer(input.Risk)
		}
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("create evaluation audit todo_id=%d: %w", todo.ID, err)
		}
		// auto route: Codex judged the clue ready, so the system creates the Task
		// itself (no human confirmation) and the Todo lands on "auto". The pipeline
		// immediately wakes M5; its cron remains the recovery path. The confirmed
		// plan is Codex's proposed_plan; the Task background is the same M3-frozen
		// context_snapshot M5 replays.
		var createdTask *domain.Task
		if input.Route == RouteAuto {
			background, err := requireContextSnapshot(&todo)
			if err != nil {
				return err
			}
			createdTask, err = createAutoTask(tx, s.now().UTC(), &todo, input.ProposedPlan, background)
			if err != nil {
				return err
			}
		}
		result = EvaluationResult{
			TodoID: todo.ID, Status: input.Route, Version: todo.Version + 1,
			Confidence: input.Confidence, Risk: input.Risk,
		}
		if createdTask != nil {
			result.TaskID = &createdTask.ID
			result.TaskVersion = createdTask.Version
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func validateEvaluationInput(input EvaluationInput) error {
	if input.TodoID == 0 || input.ExpectedVersion < 0 {
		return fmt.Errorf("%w: evaluation Todo ID/version is invalid", ErrInvalidInput)
	}
	switch input.Route {
	case RouteAuto, RouteNeedInfo, RouteNeedDecision, RouteDropped:
	default:
		return fmt.Errorf("%w: evaluation route must be auto, need_info, need_decision or dropped", ErrInvalidInput)
	}
	if input.Route == RouteAuto && input.ProposedPlan == nil {
		return fmt.Errorf("%w: auto route requires a proposed plan", ErrInvalidInput)
	}
	if strings.TrimSpace(input.RouteReason) == "" {
		return fmt.Errorf("%w: evaluation route reason is blank", ErrInvalidInput)
	}
	if input.ManualGate {
		if input.Route != RouteNeedDecision || input.DecisionEngine != DecisionEngineManual {
			return fmt.Errorf("%w: manual gate must route to need_decision with manual engine", ErrInvalidInput)
		}
		if len(input.ConfidenceFactors) != 0 || len(input.RiskFactors) != 0 || input.ProposedPlan != nil || input.CodexSessionID != nil || input.PromptVersion != "" {
			return fmt.Errorf("%w: manual gate must not contain scoring or Codex data", ErrInvalidInput)
		}
	} else {
		if err := validateRuleScore(RuleScore{Confidence: input.Confidence, Risk: input.Risk}); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		if err := validateFactors("confidence", input.ConfidenceFactors); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		if err := validateFactors("risk", input.RiskFactors); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
	}
	if len(input.MatchedRules) == 0 {
		return fmt.Errorf("%w: evaluation matched rules is empty", ErrInvalidInput)
	}
	for position, rule := range input.MatchedRules {
		if strings.TrimSpace(rule) == "" {
			return fmt.Errorf("%w: evaluation matched_rules[%d] is blank", ErrInvalidInput, position)
		}
	}
	if input.DecisionEngine != DecisionEngineRule && input.DecisionEngine != DecisionEngineCodex && input.DecisionEngine != DecisionEngineManual {
		return fmt.Errorf("%w: evaluation decision engine is unsupported", ErrInvalidInput)
	}
	if input.DecisionEngine == DecisionEngineManual && !input.ManualGate {
		return fmt.Errorf("%w: manual decision engine requires manual gate", ErrInvalidInput)
	}
	if strings.TrimSpace(input.ThresholdConfigVersion) == "" {
		return fmt.Errorf("%w: evaluation threshold config version is blank", ErrInvalidInput)
	}
	if input.DecisionEngine == DecisionEngineCodex {
		if strings.TrimSpace(input.PromptVersion) == "" {
			return fmt.Errorf("%w: Codex evaluation prompt version is blank", ErrInvalidInput)
		}
		if (input.CodexSessionID == nil || strings.TrimSpace(*input.CodexSessionID) == "") && strings.TrimSpace(input.FailureDetail) == "" {
			return fmt.Errorf("%w: Codex evaluation requires session ID or failure detail", ErrInvalidInput)
		}
	}
	if input.ProposedPlan != nil {
		if err := validatePlanDraft(input.ProposedPlan); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
	}
	return nil
}

// createAutoTask creates the Task for an auto-routed Todo inside the evaluation
// transaction. It mirrors Service.Approve's task creation but records the system
// (m4_auto) as the confirmer instead of a human. The plan is Codex's
// proposed_plan serialized as the confirmed plan JSON.
func createAutoTask(tx *gorm.DB, now time.Time, todo *domain.Todo, plan *PlanDraft, background json.RawMessage) (*domain.Task, error) {
	if plan == nil {
		return nil, fmt.Errorf("%w: auto task todo_id=%d has no proposed plan", ErrInvalidInput, todo.ID)
	}
	planRaw, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("encode auto task plan todo_id=%d: %w", todo.ID, err)
	}
	planJSON, err := canonicalJSONObject(planRaw, "plan")
	if err != nil {
		return nil, err
	}
	var existing domain.Task
	found := tx.Where("todo_id = ?", todo.ID).Limit(1).Find(&existing)
	if found.Error != nil {
		return nil, fmt.Errorf("check existing Task todo_id=%d: %w", todo.ID, found.Error)
	}
	if found.RowsAffected != 0 {
		return nil, fmt.Errorf("%w: todo_id=%d task_id=%d", ErrTaskExists, todo.ID, existing.ID)
	}
	factory, err := taskcreate.NewFactory(tx)
	if err != nil {
		return nil, err
	}
	todoID := todo.ID
	task, err := factory.CreateWithDB(context.Background(), tx, taskcreate.Input{
		TodoID: &todoID, Title: todo.Title, ActionType: todo.ActionType, Target: todo.Target,
		Background: background, Plan: planJSON, ConfirmedBy: "m4_auto", ConfirmedAt: &now,
		ProjectID: copyUint64(todo.ProjectID), SourceType: taskcreate.SourceTodo, SourceID: &todoID,
		ExecutionMode: taskcreate.ExecutionModeStandard, ActorType: "m4",
	})
	if errors.Is(err, taskcreate.ErrExists) {
		return nil, fmt.Errorf("%w: todo_id=%d", ErrTaskExists, todo.ID)
	}
	if err != nil {
		return nil, err
	}
	if err := createTodoEvent(tx, todo.ID, RouteAuto, RouteAuto, map[string]any{
		"event_type": "auto_task_created", "task_id": task.ID,
	}); err != nil {
		return nil, err
	}
	return task, nil
}

func float64Pointer(value float64) *float64 {
	copy := value
	return &copy
}
