package okrworkspace

import (
	"errors"
	"reflect"
	"testing"
)

func TestReplaceRegionalMatchesIsAtomicCompleteAndIdempotent(t *testing.T) {
	service, err := NewService(openWorkspaceTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreatePlan(t.Context(), CreatePlanInput{Quarter: "2026-Q4", Title: "Q4 Plan", CreatedBy: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePlanObjective(t.Context(), plan.ID, PlanObjectiveView{ID: "plan-o", Title: "目标", KRs: []PlanKRView{{ID: "plan-kr-1", Title: "KR 1"}, {ID: "plan-kr-2", Title: "KR 2"}}}, "owner"); err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateRegionalDemand(t.Context(), "2026-Q4", "eu", "owner", RegionalDemandInput{RegionalOKR: "O1", Requirement: "需求一", Acceptance: "yes", PlanKRIDs: []string{"plan-kr-1"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateRegionalDemand(t.Context(), "2026-Q4", "eu", "owner", RegionalDemandInput{RegionalOKR: "O2", Requirement: "需求二", Acceptance: "tbd", SortOrder: 1})
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.RegionalAlignmentBoard(t.Context(), "2026-Q4", "eu", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if before.MatchVersion == "" {
		t.Fatal("board match_version is empty")
	}

	result, err := service.ReplaceRegionalMatches(t.Context(), "2026-Q4", "eu", "agent", ReplaceRegionalMatchesInput{
		ExpectedMatchVersion: before.MatchVersion,
		Items: []RegionalMatchItem{
			{DemandID: first.ID, PlanKRIDs: []string{"plan-kr-2"}},
			{DemandID: second.ID, PlanKRIDs: []string{"plan-kr-2", "plan-kr-1", "plan-kr-2"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedDemands != 2 || result.UnchangedDemands != 0 || result.AddedLinks != 3 || result.RemovedLinks != 1 {
		t.Fatalf("replacement result = %+v", result)
	}
	after, err := service.RegionalAlignmentBoard(t.Context(), "2026-Q4", "eu", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if after.MatchVersion != result.MatchVersion || after.MatchVersion == before.MatchVersion {
		t.Fatalf("match versions before=%q result=%q after=%q", before.MatchVersion, result.MatchVersion, after.MatchVersion)
	}
	if !reflect.DeepEqual(after.Demands[0].PlanKRIDs, []string{"plan-kr-2"}) || !reflect.DeepEqual(after.Demands[1].PlanKRIDs, []string{"plan-kr-1", "plan-kr-2"}) {
		t.Fatalf("updated matches = %+v", after.Demands)
	}
	if after.Demands[0].RegionalOKR != first.RegionalOKR || after.Demands[0].Acceptance != first.Acceptance || after.Demands[1].Requirement != second.Requirement {
		t.Fatalf("replacement changed non-match fields: %+v", after.Demands)
	}

	noOp, err := service.ReplaceRegionalMatches(t.Context(), "2026-Q4", "eu", "agent", ReplaceRegionalMatchesInput{
		ExpectedMatchVersion: after.MatchVersion,
		Items:                []RegionalMatchItem{{DemandID: first.ID, PlanKRIDs: []string{"plan-kr-2"}}, {DemandID: second.ID, PlanKRIDs: []string{"plan-kr-1", "plan-kr-2"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if noOp.ChangedDemands != 0 || noOp.UnchangedDemands != 2 || noOp.MatchVersion != after.MatchVersion {
		t.Fatalf("idempotent replacement = %+v", noOp)
	}

	if _, err := service.ReplaceRegionalMatches(t.Context(), "2026-Q4", "eu", "agent", ReplaceRegionalMatchesInput{ExpectedMatchVersion: before.MatchVersion, Items: []RegionalMatchItem{{DemandID: first.ID}, {DemandID: second.ID}}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale replacement error = %v, want ErrConflict", err)
	}
	if _, err := service.ReplaceRegionalMatches(t.Context(), "2026-Q4", "eu", "agent", ReplaceRegionalMatchesInput{ExpectedMatchVersion: after.MatchVersion, Items: []RegionalMatchItem{{DemandID: first.ID}}}); err == nil {
		t.Fatal("incomplete replacement unexpectedly succeeded")
	}
	final, err := service.RegionalAlignmentBoard(t.Context(), "2026-Q4", "eu", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if final.MatchVersion != after.MatchVersion {
		t.Fatalf("failed replacements changed board: before=%q after=%q", after.MatchVersion, final.MatchVersion)
	}
}
