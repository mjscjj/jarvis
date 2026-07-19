package decide

import (
	"math"
	"testing"
)

func TestRouterFirstMatchWins(t *testing.T) {
	router := fixtureRouter(t)
	tests := []struct {
		name   string
		todo   ScoredTodo
		route  string
		reason string
	}{
		{name: "scoring failure", todo: ScoredTodo{ActionType: "investigate", ScoringFailed: true}, route: RouteNeedDecision, reason: "scoring_failed"},
		{name: "unknown action", todo: ScoredTodo{ActionType: "unknown", Confidence: 1}, route: RouteNeedDecision, reason: "unknown_action_type"},
		{name: "force confirm", todo: ScoredTodo{ActionType: "reply_message", Confidence: 1}, route: RouteNeedDecision, reason: "action_force_confirm"},
		{name: "plan unclear before missing slot", todo: ScoredTodo{ActionType: "investigate", Confidence: 0.9, Risk: 0.1, HasMissingSlot: true, Blocker: BlockerPlanUnclear}, route: RouteNeedDecision, reason: "plan_unclear"},
		{name: "missing slot", todo: ScoredTodo{ActionType: "investigate", Confidence: 0.2, Risk: 0.1, HasMissingSlot: true, Blocker: BlockerInfoGap}, route: RouteNeedInfo, reason: "missing_required_slot"},
		{name: "risk gate", todo: ScoredTodo{ActionType: "investigate", Confidence: 1, Risk: 0.6}, route: RouteNeedDecision, reason: "risk_gate"},
		{name: "auto candidate", todo: ScoredTodo{ActionType: "investigate", Confidence: 0.85, Risk: 0.2}, route: RouteAuto, reason: "high_confidence_low_risk"},
		{name: "recommended review", todo: ScoredTodo{ActionType: "investigate", Confidence: 0.9, Risk: 0.2, RecommendedReview: true}, route: RouteNeedDecision, reason: "default_safe_route"},
		{name: "uncertainty review", todo: ScoredTodo{ActionType: "investigate", Confidence: 0.9, Risk: 0.2, UncertaintyFactors: []string{"a", "b"}}, route: RouteNeedDecision, reason: "default_safe_route"},
		{name: "low confidence info gap", todo: ScoredTodo{ActionType: "investigate", Confidence: 0.5, Risk: 0.3, Blocker: BlockerInfoGap}, route: RouteNeedInfo, reason: "low_confidence_info_gap"},
		{name: "default safe", todo: ScoredTodo{ActionType: "investigate", Confidence: 0.7, Risk: 0.2}, route: RouteNeedDecision, reason: "default_safe_route"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := router.Route(test.todo)
			if err != nil {
				t.Fatalf("Route() error = %v", err)
			}
			if result.Route != test.route || result.Reason != test.reason || len(result.Rules) == 0 {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestRouterUsesInclusiveRiskGateAndExclusiveAutoRisk(t *testing.T) {
	router := fixtureRouter(t)
	result, err := router.Route(ScoredTodo{ActionType: "investigate", Confidence: 1, Risk: 0.25})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Route != RouteNeedDecision {
		t.Fatalf("risk at auto ceiling route = %q", result.Route)
	}
	result, err = router.Route(ScoredTodo{ActionType: "investigate", Confidence: 1, Risk: 0.6})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Route != RouteNeedDecision || result.Reason != "risk_gate" {
		t.Fatalf("risk at gate result = %#v", result)
	}
}

func TestRouterCopiesActionManifest(t *testing.T) {
	actions := map[string]ActionPolicy{"investigate": {Severity: "low", Reversible: true, TTLHours: 24}}
	router, err := NewRouter(fixtureRouterOptions(actions))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	actions["investigate"] = ActionPolicy{Severity: "high", ForceConfirm: true, TTLHours: 1}
	result, err := router.Route(ScoredTodo{ActionType: "investigate", Confidence: 0.9, Risk: 0.1})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if result.Route != RouteAuto {
		t.Fatalf("mutated source manifest changed route: %#v", result)
	}
}

func TestNewRouterValidation(t *testing.T) {
	validActions := map[string]ActionPolicy{"investigate": {Severity: "low", Reversible: true, TTLHours: 24}}
	tests := []RouterOptions{
		{},
		fixtureRouterOptions(nil),
		func() RouterOptions {
			options := fixtureRouterOptions(validActions)
			options.RiskGate = 2
			return options
		}(),
		func() RouterOptions {
			options := fixtureRouterOptions(validActions)
			options.InfoConfidenceFloor = 0.9
			return options
		}(),
		func() RouterOptions {
			options := fixtureRouterOptions(validActions)
			options.UncertaintyReviewCount = 0
			return options
		}(),
		func() RouterOptions {
			options := fixtureRouterOptions(validActions)
			options.AutoConfidenceFloor = math.NaN()
			return options
		}(),
	}
	for _, options := range tests {
		if _, err := NewRouter(options); err == nil {
			t.Fatalf("NewRouter(%#v) succeeded", options)
		}
	}
}

func fixtureRouter(t *testing.T) *Router {
	t.Helper()
	router, err := NewRouter(fixtureRouterOptions(map[string]ActionPolicy{
		"investigate":   {Severity: "low", Reversible: true, TTLHours: 24},
		"reply_message": {Severity: "high", Reversible: false, ForceConfirm: true, TTLHours: 24},
	}))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}

func fixtureRouterOptions(actions map[string]ActionPolicy) RouterOptions {
	return RouterOptions{
		AutoConfidenceFloor: 0.85, RiskAutoCeiling: 0.25, InfoConfidenceFloor: 0.6,
		RiskGate: 0.6, UncertaintyReviewCount: 2, Actions: actions,
	}
}
