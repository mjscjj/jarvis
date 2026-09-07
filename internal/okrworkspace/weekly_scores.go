package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

type WeeklyScoreInput struct {
	Quarter         string                       `json:"quarter"`
	Week            string                       `json:"week"`
	TargetKind      domain.WeeklyScoreTargetKind `json:"target_kind"`
	TargetID        string                       `json:"target_id"`
	Score           float64                      `json:"score"`
	ExpectedVersion int32                        `json:"expected_version"`
	UpdatedBy       string                       `json:"updated_by"`
}

type DeleteWeeklyScoreInput struct {
	Quarter         string                       `json:"quarter"`
	Week            string                       `json:"week"`
	TargetKind      domain.WeeklyScoreTargetKind `json:"target_kind"`
	TargetID        string                       `json:"target_id"`
	ExpectedVersion int32                        `json:"expected_version"`
	UpdatedBy       string                       `json:"updated_by"`
}

func (s *Service) ReplaceWeeklyScore(ctx context.Context, input WeeklyScoreInput) (KRView, error) {
	input.Quarter = strings.TrimSpace(input.Quarter)
	input.Week = strings.TrimSpace(input.Week)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	if err := validateWeeklyScoreInput(input); err != nil {
		return KRView{}, err
	}
	krID, err := s.requirePreviewScoreTarget(ctx, input.Quarter, input.Week, input.TargetKind, input.TargetID)
	if err != nil {
		return KRView{}, err
	}

	var existing domain.WeeklyScore
	err = s.db.WithContext(ctx).First(&existing,
		"quarter = ? AND week = ? AND target_kind = ? AND target_id = ?",
		input.Quarter, input.Week, input.TargetKind, input.TargetID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if input.ExpectedVersion != 0 {
			return KRView{}, ErrConflict
		}
		now := time.Now().UTC()
		row := domain.WeeklyScore{
			Quarter: input.Quarter, Week: input.Week, TargetKind: input.TargetKind, TargetID: input.TargetID,
			Score: input.Score, Version: 0, UpdatedBy: input.UpdatedBy, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return KRView{}, fmt.Errorf("create weekly score: %w", err)
		}
	} else if err != nil {
		return KRView{}, fmt.Errorf("read weekly score: %w", err)
	} else {
		result := s.db.WithContext(ctx).Model(&domain.WeeklyScore{}).
			Where("quarter = ? AND week = ? AND target_kind = ? AND target_id = ? AND version = ?",
				input.Quarter, input.Week, input.TargetKind, input.TargetID, input.ExpectedVersion).
			Updates(map[string]any{
				"score": input.Score, "updated_by": input.UpdatedBy, "updated_at": time.Now().UTC(),
				"version": gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return KRView{}, fmt.Errorf("update weekly score: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return KRView{}, ErrConflict
		}
	}
	return s.GetKR(ctx, krID, input.Week)
}

func (s *Service) DeleteWeeklyScore(ctx context.Context, input DeleteWeeklyScoreInput) (KRView, error) {
	input.Quarter = strings.TrimSpace(input.Quarter)
	input.Week = strings.TrimSpace(input.Week)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	if input.ExpectedVersion < 0 {
		return KRView{}, fmt.Errorf("expected_version must be non-negative")
	}
	if input.UpdatedBy == "" {
		return KRView{}, fmt.Errorf("updated_by is required")
	}
	krID, err := s.requirePreviewScoreTarget(ctx, input.Quarter, input.Week, input.TargetKind, input.TargetID)
	if err != nil {
		return KRView{}, err
	}
	result := s.db.WithContext(ctx).
		Where("quarter = ? AND week = ? AND target_kind = ? AND target_id = ? AND version = ?",
			input.Quarter, input.Week, input.TargetKind, input.TargetID, input.ExpectedVersion).
		Delete(&domain.WeeklyScore{})
	if result.Error != nil {
		return KRView{}, fmt.Errorf("delete weekly score: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&domain.WeeklyScore{}).
			Where("quarter = ? AND week = ? AND target_kind = ? AND target_id = ?",
				input.Quarter, input.Week, input.TargetKind, input.TargetID).
			Count(&count).Error; err != nil {
			return KRView{}, fmt.Errorf("check weekly score conflict: %w", err)
		}
		if count == 0 {
			return KRView{}, ErrNotFound
		}
		return KRView{}, ErrConflict
	}
	return s.GetKR(ctx, krID, input.Week)
}

func (s *Service) requirePreviewScoreTarget(ctx context.Context, quarter, week string, kind domain.WeeklyScoreTargetKind, targetID string) (string, error) {
	if !quarterPattern.MatchString(quarter) || !weekPattern.MatchString(week) || !domain.ValidWeeklyScoreTargetKind(kind) || targetID == "" {
		return "", fmt.Errorf("quarter, week, target_kind and target_id are required")
	}
	opened, err := s.openedWeek(ctx, quarter, week)
	if err != nil {
		return "", err
	}
	if opened.TemplateKey != domain.WeekTemplateOKRPreview {
		return "", fmt.Errorf("weekly scores require template %q", domain.WeekTemplateOKRPreview)
	}

	var kr domain.KR
	switch kind {
	case domain.WeeklyScoreTargetKR:
		if err := s.db.WithContext(ctx).First(&kr, "id = ?", targetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", ErrNotFound
			}
			return "", fmt.Errorf("get weekly score KR: %w", err)
		}
	case domain.WeeklyScoreTargetPoint:
		var point domain.KRPoint
		if err := s.db.WithContext(ctx).First(&point, "id = ?", targetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", ErrNotFound
			}
			return "", fmt.Errorf("get weekly score point: %w", err)
		}
		if err := s.db.WithContext(ctx).First(&kr, "id = ?", point.KRID).Error; err != nil {
			return "", fmt.Errorf("get KR for weekly score point: %w", err)
		}
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ?", kr.ObjectiveID).Error; err != nil {
		return "", fmt.Errorf("get objective for weekly score target: %w", err)
	}
	if objective.PlanID != "" {
		return "", ErrNotFound
	}
	if objective.Quarter != quarter {
		return "", fmt.Errorf("weekly score target does not belong to quarter %s", quarter)
	}
	return kr.ID, nil
}

func validateWeeklyScoreInput(input WeeklyScoreInput) error {
	if input.ExpectedVersion < 0 {
		return fmt.Errorf("expected_version must be non-negative")
	}
	if input.UpdatedBy == "" {
		return fmt.Errorf("updated_by is required")
	}
	if math.IsNaN(input.Score) || math.IsInf(input.Score, 0) || input.Score < 0 || input.Score > 1 || math.Abs(input.Score*10-math.Round(input.Score*10)) > 1e-9 {
		return fmt.Errorf("score must be between 0.0 and 1.0 in 0.1 increments")
	}
	return nil
}

func validateStoredWeeklyScore(score domain.WeeklyScore) error {
	if !domain.ValidWeeklyScoreTargetKind(score.TargetKind) || score.TargetID == "" || score.Version < 0 || math.IsNaN(score.Score) || math.IsInf(score.Score, 0) || score.Score < 0 || score.Score > 1 || math.Abs(score.Score*10-math.Round(score.Score*10)) > 1e-9 {
		return fmt.Errorf("invalid weekly score in storage for %s/%s %s:%s", score.Quarter, score.Week, score.TargetKind, score.TargetID)
	}
	return nil
}
