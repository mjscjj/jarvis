package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"jarvis/internal/domain"
)

const (
	DecisionEngineRule          = "rule"
	DecisionEngineCodex         = "codex"
	DecisionEngineManual        = "manual"
	DecisionRouteNeedDecision   = "need_decision"
	BudgetRouteNeedDecision     = "route_need_decision"
	BudgetDegradeToRule         = "degrade_to_rule"
	DeepJudgeReasonOutsideGray  = "outside_gray_zone"
	DeepJudgeReasonCodex        = "codex_decided"
	DeepJudgeReasonBudget       = "codex_budget_exceeded"
	DeepJudgeReasonTimeout      = "codex_timeout"
	DeepJudgeReasonCodexFailure = "codex_failed"
)

type GrayZone struct {
	ConfidenceLow  float64
	ConfidenceHigh float64
	RiskLow        float64
	RiskHigh       float64
}

type RuleScore struct {
	Confidence float64 `json:"confidence"`
	Risk       float64 `json:"risk"`
}

type DeepJudgeResult struct {
	Engine        string       `json:"engine"`
	RouteOverride string       `json:"route_override,omitempty"`
	Reason        string       `json:"reason"`
	FailureDetail string       `json:"failure_detail,omitempty"`
	PromptVersion string       `json:"prompt_version,omitempty"`
	Codex         *CodexResult `json:"codex,omitempty"`
}

type DeepJudgeInput struct {
	Todo       *domain.Todo
	Background json.RawMessage
	RepoPath   string
}

type codexDecisionRunner interface {
	Decide(context.Context, CodexInput) (*CodexResult, error)
}

type DeepJudge struct {
	codex            codexDecisionRunner
	grayZone         GrayZone
	onBudgetExceeded string
}

func NewDeepJudge(codex codexDecisionRunner, grayZone GrayZone, onBudgetExceeded string) (*DeepJudge, error) {
	if codex == nil {
		return nil, fmt.Errorf("deep judge codex decider is nil")
	}
	if err := validateGrayZone(grayZone); err != nil {
		return nil, err
	}
	if onBudgetExceeded != BudgetRouteNeedDecision && onBudgetExceeded != BudgetDegradeToRule {
		return nil, fmt.Errorf("deep judge unsupported budget behavior %q", onBudgetExceeded)
	}
	return &DeepJudge{codex: codex, grayZone: grayZone, onBudgetExceeded: onBudgetExceeded}, nil
}

func (j *DeepJudge) Judge(ctx context.Context, score RuleScore, input DeepJudgeInput) (*DeepJudgeResult, error) {
	if err := validateRuleScore(score); err != nil {
		return nil, err
	}
	if !j.inGrayZone(score) {
		return &DeepJudgeResult{Engine: DecisionEngineRule, Reason: DeepJudgeReasonOutsideGray}, nil
	}
	prompt, err := BuildCodexPrompt(CodexPromptInput{Todo: input.Todo, RuleScore: score, Background: input.Background})
	if err != nil {
		return nil, err
	}
	result, err := j.codex.Decide(ctx, CodexInput{Prompt: prompt.Text, RepoPath: input.RepoPath})
	if err == nil {
		if result == nil {
			return &DeepJudgeResult{
				Engine: DecisionEngineCodex, RouteOverride: DecisionRouteNeedDecision,
				Reason: DeepJudgeReasonCodexFailure, FailureDetail: "codex decider returned nil result", PromptVersion: prompt.Version,
			}, nil
		}
		return &DeepJudgeResult{Engine: DecisionEngineCodex, Reason: DeepJudgeReasonCodex, Codex: result, PromptVersion: prompt.Version}, nil
	}
	if errors.Is(err, ErrCodexBudgetExceeded) {
		if j.onBudgetExceeded == BudgetDegradeToRule {
			return &DeepJudgeResult{
				Engine: DecisionEngineRule, Reason: DeepJudgeReasonBudget, FailureDetail: err.Error(), PromptVersion: prompt.Version,
			}, nil
		}
		return &DeepJudgeResult{
			Engine: DecisionEngineCodex, RouteOverride: DecisionRouteNeedDecision,
			Reason: DeepJudgeReasonBudget, FailureDetail: err.Error(), PromptVersion: prompt.Version,
		}, nil
	}
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &DeepJudgeResult{
			Engine: DecisionEngineCodex, RouteOverride: DecisionRouteNeedDecision,
			Reason: DeepJudgeReasonTimeout, FailureDetail: err.Error(), PromptVersion: prompt.Version,
		}, nil
	}
	return &DeepJudgeResult{
		Engine: DecisionEngineCodex, RouteOverride: DecisionRouteNeedDecision,
		Reason: DeepJudgeReasonCodexFailure, FailureDetail: err.Error(), PromptVersion: prompt.Version,
	}, nil
}

func (j *DeepJudge) inGrayZone(score RuleScore) bool {
	return score.Confidence >= j.grayZone.ConfidenceLow && score.Confidence <= j.grayZone.ConfidenceHigh &&
		score.Risk >= j.grayZone.RiskLow && score.Risk <= j.grayZone.RiskHigh
}

func validateGrayZone(zone GrayZone) error {
	if err := validateUnitInterval("confidence", zone.ConfidenceLow, zone.ConfidenceHigh); err != nil {
		return fmt.Errorf("deep judge gray zone: %w", err)
	}
	if err := validateUnitInterval("risk", zone.RiskLow, zone.RiskHigh); err != nil {
		return fmt.Errorf("deep judge gray zone: %w", err)
	}
	return nil
}

func validateUnitInterval(name string, low, high float64) error {
	if !unitScore(low) || !unitScore(high) {
		return fmt.Errorf("%s boundaries must be between 0 and 1", name)
	}
	if low >= high {
		return fmt.Errorf("%s low must be smaller than high", name)
	}
	return nil
}

func validateRuleScore(score RuleScore) error {
	if !unitScore(score.Confidence) {
		return fmt.Errorf("rule confidence=%v is outside [0,1]", score.Confidence)
	}
	if !unitScore(score.Risk) {
		return fmt.Errorf("rule risk=%v is outside [0,1]", score.Risk)
	}
	return nil
}

func unitScore(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}
