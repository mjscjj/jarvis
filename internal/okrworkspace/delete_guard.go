package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// deletionToken is an opaque snapshot guard for destructive operations. It is
// separate from edit versions on purpose: independent rows remain concurrently
// editable, while a stale page cannot delete a scope changed by another user.
func deletionToken(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode deletion snapshot: %w", err)
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest[:]), nil
}

func (s *Service) planDeletionToken(ctx context.Context, plan PlanView) (string, error) {
	plan.DeleteToken = ""
	var comments []domain.PageComment
	if err := s.db.WithContext(ctx).Where("plan_id = ?", plan.ID).Order("id").Find(&comments).Error; err != nil {
		return "", fmt.Errorf("list plan comments for deletion guard: %w", err)
	}
	return deletionToken(struct {
		Plan     PlanView             `json:"plan"`
		Comments []domain.PageComment `json:"comments"`
	}{Plan: plan, Comments: comments})
}

func planObjectiveStructureToken(objective PlanObjectiveView) (string, error) {
	type pointVersion struct {
		ID      string `json:"id"`
		Version int32  `json:"version"`
	}
	type krVersion struct {
		ID      string         `json:"id"`
		Version int32          `json:"version"`
		Points  []pointVersion `json:"points"`
	}
	snapshot := make([]krVersion, 0, len(objective.KRs))
	for _, kr := range objective.KRs {
		item := krVersion{ID: kr.ID, Version: kr.Version, Points: make([]pointVersion, 0, len(kr.Points))}
		for _, point := range kr.Points {
			item.Points = append(item.Points, pointVersion{ID: point.ID, Version: point.Version})
		}
		snapshot = append(snapshot, item)
	}
	return deletionToken(snapshot)
}

// planKRStructureToken guards destructive edits inside one Plan KR without
// coupling ordinary KR edits to sibling KRs in the same Objective. Point
// versions are included because point definitions are saved independently.
func planKRStructureToken(kr PlanKRView) (string, error) {
	type pointVersion struct {
		ID      string `json:"id"`
		Version int32  `json:"version"`
	}
	snapshot := struct {
		Metrics []string       `json:"metrics"`
		Points  []pointVersion `json:"points"`
	}{
		Metrics: make([]string, 0, len(kr.Metrics)),
		Points:  make([]pointVersion, 0, len(kr.Points)),
	}
	for _, metric := range kr.Metrics {
		snapshot.Metrics = append(snapshot.Metrics, metric.ID)
	}
	for _, point := range kr.Points {
		snapshot.Points = append(snapshot.Points, pointVersion{ID: point.ID, Version: point.Version})
	}
	return deletionToken(snapshot)
}

func planKRRemovesChildren(current, incoming PlanKRView) bool {
	incomingIDs := make(map[string]struct{}, len(incoming.Metrics)+len(incoming.Points))
	for _, metric := range incoming.Metrics {
		incomingIDs[metric.ID] = struct{}{}
	}
	for _, point := range incoming.Points {
		incomingIDs[point.ID] = struct{}{}
	}
	for _, metric := range current.Metrics {
		if _, exists := incomingIDs[metric.ID]; !exists {
			return true
		}
	}
	for _, point := range current.Points {
		if _, exists := incomingIDs[point.ID]; !exists {
			return true
		}
	}
	return false
}

func planObjectiveRemovesChildren(current, incoming PlanObjectiveView) bool {
	incomingKRs := make(map[string]map[string]struct{}, len(incoming.KRs))
	for _, kr := range incoming.KRs {
		points := make(map[string]struct{}, len(kr.Points))
		for _, point := range kr.Points {
			points[point.ID] = struct{}{}
		}
		incomingKRs[kr.ID] = points
	}
	for _, kr := range current.KRs {
		points, exists := incomingKRs[kr.ID]
		if !exists {
			return true
		}
		for _, point := range kr.Points {
			if _, exists := points[point.ID]; !exists {
				return true
			}
		}
	}
	return false
}

// krDefinitionRemovesChildren reports whether an aggregate replacement would
// remove a persisted metric or point. The caller uses this inside the same
// database transaction as the replacement so the snapshot check and purge are
// one short critical section.
func krDefinitionRemovesChildren(db *gorm.DB, krID string, incoming ReplaceKRInput) (bool, error) {
	var metricIDs []string
	if err := db.Model(&domain.KRMetric{}).Where("kr_id = ?", krID).Pluck("id", &metricIDs).Error; err != nil {
		return false, fmt.Errorf("list KR metrics for structure guard: %w", err)
	}
	var pointIDs []string
	if err := db.Model(&domain.KRPoint{}).Where("kr_id = ?", krID).Pluck("id", &pointIDs).Error; err != nil {
		return false, fmt.Errorf("list KR points for structure guard: %w", err)
	}
	incomingIDs := make(map[string]struct{}, len(incoming.Metrics)+len(incoming.Points))
	for _, metric := range incoming.Metrics {
		incomingIDs[metric.ID] = struct{}{}
	}
	for _, point := range incoming.Points {
		incomingIDs[point.ID] = struct{}{}
	}
	for _, id := range append(metricIDs, pointIDs...) {
		if _, kept := incomingIDs[id]; !kept {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) krDeletionToken(ctx context.Context, krID string) (string, error) {
	db := s.db.WithContext(ctx)
	var kr domain.KR
	if err := db.First(&kr, "id = ?", krID).Error; err != nil {
		return "", err
	}
	var points []domain.KRPoint
	if err := db.Where("kr_id = ?", krID).Order("id").Find(&points).Error; err != nil {
		return "", fmt.Errorf("list KR points for deletion guard: %w", err)
	}
	var metricIDs []string
	if err := db.Model(&domain.KRMetric{}).Where("kr_id = ?", krID).Order("id").Pluck("id", &metricIDs).Error; err != nil {
		return "", fmt.Errorf("list KR metrics for deletion guard: %w", err)
	}
	pointIDs := make([]string, 0, len(points))
	for _, point := range points {
		pointIDs = append(pointIDs, point.ID)
	}

	snapshot := struct {
		KR             domain.KR                  `json:"kr"`
		Points         []domain.KRPoint           `json:"points"`
		Cores          []domain.WeeklyKRCore      `json:"cores"`
		Progress       []domain.KRProgress        `json:"progress"`
		Scores         []domain.WeeklyScore       `json:"scores"`
		Comments       []domain.PageComment       `json:"comments"`
		MeegoSnapshots []domain.MeegoSyncSnapshot `json:"meego_snapshots"`
	}{KR: kr, Points: points}
	if db.Migrator().HasTable(&domain.WeeklyKRCore{}) {
		if err := db.Where("kr_id = ?", krID).Order("week").Find(&snapshot.Cores).Error; err != nil {
			return "", fmt.Errorf("list KR weekly cores for deletion guard: %w", err)
		}
	}
	if len(pointIDs) > 0 && db.Migrator().HasTable(&domain.KRProgress{}) {
		if err := db.Where("point_id IN ?", pointIDs).Order("id").Find(&snapshot.Progress).Error; err != nil {
			return "", fmt.Errorf("list KR progress for deletion guard: %w", err)
		}
	}
	if db.Migrator().HasTable(&domain.WeeklyScore{}) {
		targetIDs := append([]string{krID}, pointIDs...)
		if err := db.Where("target_id IN ?", targetIDs).Order("quarter, week, target_kind, target_id").Find(&snapshot.Scores).Error; err != nil {
			return "", fmt.Errorf("list KR scores for deletion guard: %w", err)
		}
	}
	if db.Migrator().HasTable(&domain.PageComment{}) {
		targetIDs := append(append([]string{krID}, pointIDs...), metricIDs...)
		for _, progress := range snapshot.Progress {
			targetIDs = append(targetIDs, progress.ID)
		}
		if err := db.Where("target_id IN ?", targetIDs).Order("id").Find(&snapshot.Comments).Error; err != nil {
			return "", fmt.Errorf("list KR comments for deletion guard: %w", err)
		}
	}
	if len(pointIDs) > 0 && db.Migrator().HasTable(&domain.MeegoSyncSnapshot{}) {
		if err := db.Where("point_id IN ?", pointIDs).Order("point_id").Find(&snapshot.MeegoSnapshots).Error; err != nil {
			return "", fmt.Errorf("list KR Meego snapshots for deletion guard: %w", err)
		}
	}
	return deletionToken(snapshot)
}

func (s *Service) weekDeletionToken(ctx context.Context, quarter, week string) (string, error) {
	db := s.db.WithContext(ctx)
	var anchor domain.WeeklyReportWeek
	if err := db.First(&anchor, "quarter = ? AND week = ?", quarter, week).Error; err != nil {
		return "", err
	}

	var krIDs []string
	if err := db.Model(&domain.KR{}).
		Joins("JOIN okr_workspace_objective ON okr_workspace_objective.id = okr_workspace_kr.objective_id").
		Where("okr_workspace_objective.quarter = ? AND okr_workspace_objective.plan_id = ''", quarter).
		Order("okr_workspace_kr.id").Pluck("okr_workspace_kr.id", &krIDs).Error; err != nil {
		return "", fmt.Errorf("list week KRs for deletion guard: %w", err)
	}
	var pointIDs []string
	if len(krIDs) > 0 {
		if err := db.Model(&domain.KRPoint{}).Where("kr_id IN ?", krIDs).Order("id").Pluck("id", &pointIDs).Error; err != nil {
			return "", fmt.Errorf("list week points for deletion guard: %w", err)
		}
	}

	snapshot := struct {
		Anchor          domain.WeeklyReportWeek    `json:"anchor"`
		Cores           []domain.WeeklyKRCore      `json:"cores"`
		Progress        []domain.KRProgress        `json:"progress"`
		FollowUps       []domain.FollowUpItem      `json:"follow_ups"`
		Scores          []domain.WeeklyScore       `json:"scores"`
		Comments        []domain.PageComment       `json:"comments"`
		MeegoSnapshots  []domain.MeegoSyncSnapshot `json:"meego_snapshots"`
		ReminderBatches []domain.ReminderBatch     `json:"reminder_batches"`
	}{Anchor: anchor}
	if len(krIDs) > 0 {
		if err := db.Where("kr_id IN ? AND week = ?", krIDs, week).Order("kr_id").Find(&snapshot.Cores).Error; err != nil {
			return "", fmt.Errorf("list weekly cores for deletion guard: %w", err)
		}
	}
	if len(pointIDs) > 0 {
		if err := db.Where("point_id IN ? AND week = ?", pointIDs, week).Order("id").Find(&snapshot.Progress).Error; err != nil {
			return "", fmt.Errorf("list progress for deletion guard: %w", err)
		}
		if db.Migrator().HasTable(&domain.MeegoSyncSnapshot{}) {
			if err := db.Where("point_id IN ? AND week = ?", pointIDs, week).Order("point_id").Find(&snapshot.MeegoSnapshots).Error; err != nil {
				return "", fmt.Errorf("list Meego snapshots for deletion guard: %w", err)
			}
		}
	}
	if db.Migrator().HasTable(&domain.FollowUpItem{}) {
		if err := db.Where("quarter = ? AND week = ?", quarter, week).Order("id").Find(&snapshot.FollowUps).Error; err != nil {
			return "", fmt.Errorf("list follow-ups for deletion guard: %w", err)
		}
	}
	if db.Migrator().HasTable(&domain.WeeklyScore{}) {
		if err := db.Where("quarter = ? AND week = ?", quarter, week).Order("target_kind, target_id").Find(&snapshot.Scores).Error; err != nil {
			return "", fmt.Errorf("list scores for deletion guard: %w", err)
		}
	}
	if db.Migrator().HasTable(&domain.PageComment{}) {
		if err := db.Where("quarter = ? AND week = ?", quarter, week).Order("id").Find(&snapshot.Comments).Error; err != nil {
			return "", fmt.Errorf("list comments for deletion guard: %w", err)
		}
	}
	if db.Migrator().HasTable(&domain.ReminderBatch{}) {
		if err := db.Where("quarter = ? AND week = ?", quarter, week).Order("id").Find(&snapshot.ReminderBatches).Error; err != nil {
			return "", fmt.Errorf("list reminder batches for deletion guard: %w", err)
		}
	}
	return deletionToken(snapshot)
}
