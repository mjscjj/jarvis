package okrworkspace

import (
	"errors"
	"testing"

	"jarvis/internal/okrworkspace/domain"
)

func TestWeeklyScoresArePreviewOnlyAndVersionedPerTarget(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-score", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-score", ObjectiveID: objective.ID, Title: "一级 KR"}
	strategy := domain.KRPoint{ID: "point-score-strategy", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "策略 KR"}
	product := domain.KRPoint{ID: "point-score-product", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "产品 KR"}
	classic := domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: "2026-W35", TemplateKey: domain.WeekTemplateClassic, OpenedBy: "test"}
	preview := domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: "2026-W36", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "test"}
	for _, row := range []any{&objective, &kr, &strategy, &product, &classic, &preview} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ReplaceWeeklyScore(t.Context(), WeeklyScoreInput{
		Quarter: objective.Quarter, Week: classic.Week, TargetKind: domain.WeeklyScoreTargetKR,
		TargetID: kr.ID, Score: 0.7, UpdatedBy: "ou_owner",
	}); err == nil {
		t.Fatal("classic week accepted a score")
	}
	if _, err := service.ReplaceWeeklyScore(t.Context(), WeeklyScoreInput{
		Quarter: objective.Quarter, Week: preview.Week, TargetKind: domain.WeeklyScoreTargetKR,
		TargetID: kr.ID, Score: 0.75, UpdatedBy: "ou_owner",
	}); err == nil {
		t.Fatal("score with unsupported precision succeeded")
	}

	parent, err := service.ReplaceWeeklyScore(t.Context(), WeeklyScoreInput{
		Quarter: objective.Quarter, Week: preview.Week, TargetKind: domain.WeeklyScoreTargetKR,
		TargetID: kr.ID, Score: 0.7, UpdatedBy: "ou_owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Score == nil || parent.Score.Value != 0.7 || parent.Score.Version != 1 {
		t.Fatalf("parent score = %+v", parent.Score)
	}
	pointResult, err := service.ReplaceWeeklyScore(t.Context(), WeeklyScoreInput{
		Quarter: objective.Quarter, Week: preview.Week, TargetKind: domain.WeeklyScoreTargetPoint,
		TargetID: strategy.ID, Score: 0.5, UpdatedBy: "ou_owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	pointScores := map[string]*ScoreView{}
	for _, point := range pointResult.Points {
		pointScores[point.ID] = point.Score
	}
	if pointScores[strategy.ID] == nil || pointScores[strategy.ID].Value != 0.5 || pointScores[product.ID] != nil {
		t.Fatalf("point scores = %+v", pointResult.Points)
	}
	if _, err := service.ReplaceWeeklyScore(t.Context(), WeeklyScoreInput{
		Quarter: objective.Quarter, Week: preview.Week, TargetKind: domain.WeeklyScoreTargetPoint,
		TargetID: strategy.ID, Score: 0.6, ExpectedVersion: 0, UpdatedBy: "ou_stale",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second first-write score error = %v, want ErrConflict", err)
	}
	updated, err := service.ReplaceWeeklyScore(t.Context(), WeeklyScoreInput{
		Quarter: objective.Quarter, Week: preview.Week, TargetKind: domain.WeeklyScoreTargetPoint,
		TargetID: strategy.ID, Score: 0.8, ExpectedVersion: 1, UpdatedBy: "ou_owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	pointScores = map[string]*ScoreView{}
	for _, point := range updated.Points {
		pointScores[point.ID] = point.Score
	}
	if pointScores[strategy.ID] == nil || pointScores[strategy.ID].Value != 0.8 || pointScores[strategy.ID].Version != 2 {
		t.Fatalf("updated point score = %+v", pointScores[strategy.ID])
	}
	if _, err := service.ReplaceWeeklyScore(t.Context(), WeeklyScoreInput{
		Quarter: objective.Quarter, Week: preview.Week, TargetKind: domain.WeeklyScoreTargetPoint,
		TargetID: strategy.ID, Score: 0.3, ExpectedVersion: 0, UpdatedBy: "ou_owner",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale score error = %v, want ErrConflict", err)
	}

	board, err := service.Board(t.Context(), objective.Quarter, preview.Week)
	if err != nil {
		t.Fatal(err)
	}
	boardKR := board.Objectives[0].KRs[0]
	pointScores = map[string]*ScoreView{}
	for _, point := range boardKR.Points {
		pointScores[point.ID] = point.Score
	}
	if board.TemplateKey != domain.WeekTemplateOKRPreview || boardKR.Score == nil || pointScores[strategy.ID] == nil {
		t.Fatalf("preview board scores = %+v", board)
	}
	deleted, err := service.DeleteWeeklyScore(t.Context(), DeleteWeeklyScoreInput{
		Quarter: objective.Quarter, Week: preview.Week, TargetKind: domain.WeeklyScoreTargetKR,
		TargetID: kr.ID, ExpectedVersion: 1, UpdatedBy: "ou_owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	pointScores = map[string]*ScoreView{}
	for _, point := range deleted.Points {
		pointScores[point.ID] = point.Score
	}
	if deleted.Score != nil || pointScores[strategy.ID] == nil {
		t.Fatalf("delete parent score changed wrong target: %+v", deleted)
	}
	boardBeforeWeekDelete, err := service.Board(t.Context(), objective.Quarter, preview.Week)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DeleteWeek(t.Context(), objective.Quarter, preview.Week, boardBeforeWeekDelete.DeleteToken)
	if err != nil {
		t.Fatal(err)
	}
	var retainedScores int64
	if err := db.Model(&domain.WeeklyScore{}).Where("quarter = ? AND week = ?", objective.Quarter, preview.Week).Count(&retainedScores).Error; err != nil {
		t.Fatal(err)
	}
	if retainedScores != 1 {
		t.Fatalf("retained score count = %d, want 1", retainedScores)
	}
}
