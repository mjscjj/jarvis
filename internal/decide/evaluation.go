package decide

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

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
}

type EvaluationResult struct {
	TodoID     uint64  `json:"todo_id"`
	Status     string  `json:"status"`
	Version    int32   `json:"version"`
	Confidence float64 `json:"confidence"`
	Risk       float64 `json:"risk"`
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
		update := tx.Model(&domain.Todo{}).
			Where("id = ? AND version = ? AND status = ?", todo.ID, input.ExpectedVersion, "extracted").
			Updates(map[string]any{
				"confidence": input.Confidence, "risk": input.Risk, "route": input.Route,
				"status": input.Route, "version": gorm.Expr("version + 1"),
			})
		if update.Error != nil {
			return fmt.Errorf("apply Todo evaluation id=%d: %w", todo.ID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: todo_id=%d expected_version=%d", ErrVersionConflict, todo.ID, input.ExpectedVersion)
		}
		eventDetail := map[string]any{
			"event_type": "evaluated", "route_reason": input.RouteReason,
			"confidence": input.Confidence, "risk": input.Risk,
			"confidence_factors": input.ConfidenceFactors, "risk_factors": input.RiskFactors,
			"matched_rules": input.MatchedRules, "decision_engine": input.DecisionEngine,
			"prompt_version": input.PromptVersion, "failure_detail": input.FailureDetail,
			"proposed_plan": input.ProposedPlan,
		}
		if err := createTodoEvent(tx, todo.ID, "extracted", input.Route, eventDetail); err != nil {
			return err
		}
		audit := domain.DecisionAudit{
			TodoID: todo.ID, TS: s.now().UTC(), Route: input.Route, RouteReason: input.RouteReason,
			ConfidenceEff: float64Pointer(input.Confidence), ConfidenceFactors: datatypes.JSON(confidenceFactors),
			RiskEff: float64Pointer(input.Risk), RiskFactors: datatypes.JSON(riskFactors), MatchedRules: datatypes.JSON(matchedRules),
			DecisionEngine: input.DecisionEngine, CodexSessionID: copyString(input.CodexSessionID),
			ThresholdConfigVersion: input.ThresholdConfigVersion, Channel: "auto", FinalStatus: input.Route,
		}
		if err := tx.Create(&audit).Error; err != nil {
			return fmt.Errorf("create evaluation audit todo_id=%d: %w", todo.ID, err)
		}
		result = EvaluationResult{
			TodoID: todo.ID, Status: input.Route, Version: todo.Version + 1,
			Confidence: input.Confidence, Risk: input.Risk,
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
	if err := validateRuleScore(RuleScore{Confidence: input.Confidence, Risk: input.Risk}); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if input.Route != RouteNeedInfo && input.Route != RouteNeedDecision {
		return fmt.Errorf("%w: evaluation route must be need_info or need_decision", ErrInvalidInput)
	}
	if strings.TrimSpace(input.RouteReason) == "" {
		return fmt.Errorf("%w: evaluation route reason is blank", ErrInvalidInput)
	}
	if err := validateFactors("confidence", input.ConfidenceFactors); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := validateFactors("risk", input.RiskFactors); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if len(input.MatchedRules) == 0 {
		return fmt.Errorf("%w: evaluation matched rules is empty", ErrInvalidInput)
	}
	for position, rule := range input.MatchedRules {
		if strings.TrimSpace(rule) == "" {
			return fmt.Errorf("%w: evaluation matched_rules[%d] is blank", ErrInvalidInput, position)
		}
	}
	if input.DecisionEngine != DecisionEngineRule && input.DecisionEngine != DecisionEngineCodex {
		return fmt.Errorf("%w: evaluation decision engine is unsupported", ErrInvalidInput)
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

func float64Pointer(value float64) *float64 {
	copy := value
	return &copy
}
