package decide

import (
	"fmt"
	"strings"
)

const (
	RouteAuto         = "auto"
	RouteNeedInfo     = "need_info"
	RouteNeedDecision = "need_decision"

	BlockerNone        = ""
	BlockerInfoGap     = "info_gap"
	BlockerPlanUnclear = "plan_unclear"
)

type ActionPolicy struct {
	Severity     string
	Reversible   bool
	ForceConfirm bool
	TTLHours     int
}

type RouterOptions struct {
	AutoConfidenceFloor    float64
	RiskAutoCeiling        float64
	InfoConfidenceFloor    float64
	RiskGate               float64
	UncertaintyReviewCount int
	Actions                map[string]ActionPolicy
}

type ScoredTodo struct {
	ActionType         string
	Confidence         float64
	Risk               float64
	ScoringFailed      bool
	HasMissingSlot     bool
	Blocker            string
	RecommendedReview  bool
	UncertaintyFactors []string
}

type RouteResult struct {
	Route  string   `json:"route"`
	Reason string   `json:"reason"`
	Rules  []string `json:"rules"`
}

type Router struct {
	opts RouterOptions
}

func NewRouter(opts RouterOptions) (*Router, error) {
	if err := validateRouterOptions(opts); err != nil {
		return nil, err
	}
	actions := make(map[string]ActionPolicy, len(opts.Actions))
	for actionType, policy := range opts.Actions {
		actions[actionType] = policy
	}
	opts.Actions = actions
	return &Router{opts: opts}, nil
}

func (r *Router) Route(todo ScoredTodo) (*RouteResult, error) {
	if strings.TrimSpace(todo.ActionType) == "" {
		return nil, fmt.Errorf("route Todo action_type is blank")
	}
	if err := validateRuleScore(RuleScore{Confidence: todo.Confidence, Risk: todo.Risk}); err != nil {
		return nil, err
	}
	switch todo.Blocker {
	case BlockerNone, BlockerInfoGap, BlockerPlanUnclear:
	default:
		return nil, fmt.Errorf("route Todo has unsupported blocker %q", todo.Blocker)
	}
	for position, factor := range todo.UncertaintyFactors {
		if strings.TrimSpace(factor) == "" {
			return nil, fmt.Errorf("route Todo uncertainty_factors[%d] is blank", position)
		}
	}

	if todo.ScoringFailed {
		return routeResult(RouteNeedDecision, "scoring_failed", "scoring_failed"), nil
	}
	policy, known := r.opts.Actions[todo.ActionType]
	if !known {
		return routeResult(RouteNeedDecision, "unknown_action_type", "unknown_action_type"), nil
	}
	if policy.ForceConfirm {
		return routeResult(RouteNeedDecision, "action_force_confirm", "action_manifest.force_confirm"), nil
	}
	if todo.Blocker == BlockerPlanUnclear {
		return routeResult(RouteNeedDecision, "plan_unclear", "blocker.plan_unclear"), nil
	}
	if todo.HasMissingSlot && todo.Risk < r.opts.RiskGate {
		return routeResult(RouteNeedInfo, "missing_required_slot", "blocker.info_gap"), nil
	}
	if todo.Risk >= r.opts.RiskGate {
		return routeResult(RouteNeedDecision, "risk_gate", "threshold.risk_gate"), nil
	}
	reviewByUncertainty := len(todo.UncertaintyFactors) >= r.opts.UncertaintyReviewCount
	if todo.Confidence >= r.opts.AutoConfidenceFloor &&
		todo.Risk < r.opts.RiskAutoCeiling &&
		!todo.RecommendedReview && !reviewByUncertainty {
		return routeResult(RouteAuto, "high_confidence_low_risk", "threshold.auto_confidence", "threshold.risk_auto"), nil
	}
	if todo.Confidence < r.opts.InfoConfidenceFloor && todo.Blocker == BlockerInfoGap {
		return routeResult(RouteNeedInfo, "low_confidence_info_gap", "threshold.info_confidence", "blocker.info_gap"), nil
	}
	rules := []string{"default.need_decision"}
	if todo.RecommendedReview {
		rules = append(rules, "recommended_review")
	}
	if reviewByUncertainty {
		rules = append(rules, "uncertainty_count")
	}
	return routeResult(RouteNeedDecision, "default_safe_route", rules...), nil
}

func validateRouterOptions(opts RouterOptions) error {
	for name, value := range map[string]float64{
		"auto_confidence_floor": opts.AutoConfidenceFloor,
		"risk_auto_ceiling":     opts.RiskAutoCeiling,
		"info_confidence_floor": opts.InfoConfidenceFloor,
		"risk_gate":             opts.RiskGate,
	} {
		if !unitScore(value) {
			return fmt.Errorf("router %s=%v is outside [0,1]", name, value)
		}
	}
	if opts.InfoConfidenceFloor > opts.AutoConfidenceFloor {
		return fmt.Errorf("router info confidence floor must not exceed auto confidence floor")
	}
	if opts.RiskAutoCeiling > opts.RiskGate {
		return fmt.Errorf("router risk auto ceiling must not exceed risk gate")
	}
	if opts.UncertaintyReviewCount <= 0 {
		return fmt.Errorf("router uncertainty review count must be positive")
	}
	if len(opts.Actions) == 0 {
		return fmt.Errorf("router action manifest is empty")
	}
	for actionType, policy := range opts.Actions {
		if strings.TrimSpace(actionType) == "" {
			return fmt.Errorf("router action manifest contains blank action_type")
		}
		switch policy.Severity {
		case "low", "medium", "high":
		default:
			return fmt.Errorf("router action %q has unsupported severity %q", actionType, policy.Severity)
		}
		if policy.TTLHours <= 0 {
			return fmt.Errorf("router action %q ttl_hours must be positive", actionType)
		}
	}
	return nil
}

func routeResult(route, reason string, rules ...string) *RouteResult {
	return &RouteResult{Route: route, Reason: reason, Rules: append([]string(nil), rules...)}
}
