package okrworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

type FollowUpInput struct {
	ID              string                 `json:"id"`
	ExpectedVersion int32                  `json:"expected_version"`
	Quarter         string                 `json:"quarter"`
	Week            string                 `json:"week"`
	Topic           string                 `json:"topic"`
	Owners          []domain.FollowUpOwner `json:"owners"`
	Status          domain.FollowUpStatus  `json:"status"`
	AssignDate      string                 `json:"assign_date"`
	Update          string                 `json:"update"`
	SourceKey       string                 `json:"source_key"`
	SourcePayload   datatypes.JSON         `json:"source_payload"`
	SortOrder       int                    `json:"sort_order"`
	UpdatedBy       string                 `json:"-"`
}

type DeleteFollowUpInput struct {
	ExpectedVersion int32 `json:"expected_version"`
}

type FollowUpView struct {
	ID            string                 `json:"id"`
	Quarter       string                 `json:"quarter"`
	Week          string                 `json:"week"`
	Version       int32                  `json:"version"`
	Topic         string                 `json:"topic"`
	Owners        []domain.FollowUpOwner `json:"owners"`
	Status        domain.FollowUpStatus  `json:"status"`
	AssignDate    string                 `json:"assign_date"`
	Update        string                 `json:"update"`
	SourceKey     string                 `json:"source_key,omitempty"`
	SourcePayload datatypes.JSON         `json:"source_payload"`
	SortOrder     int                    `json:"sort_order"`
	CreatedBy     string                 `json:"created_by"`
	UpdatedBy     string                 `json:"updated_by"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

type FollowUpList struct {
	Quarter string         `json:"quarter"`
	Week    string         `json:"week"`
	Count   int            `json:"count"`
	Items   []FollowUpView `json:"items"`
}

func (s *Service) FollowUps(ctx context.Context, quarter, week string) (FollowUpList, error) {
	quarter, week = strings.TrimSpace(quarter), strings.TrimSpace(week)
	if err := s.requireOpenWeek(ctx, quarter, week); err != nil {
		return FollowUpList{}, err
	}
	var rows []domain.FollowUpItem
	if err := s.db.WithContext(ctx).Where("quarter = ? AND week = ?", quarter, week).
		Order("CASE WHEN assign_date = '' THEN 1 ELSE 0 END ASC").
		Order("assign_date ASC, sort_order ASC, created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return FollowUpList{}, fmt.Errorf("list follow-up items: %w", err)
	}
	items := make([]FollowUpView, 0, len(rows))
	for _, row := range rows {
		items = append(items, followUpView(row))
	}
	return FollowUpList{Quarter: quarter, Week: week, Count: len(items), Items: items}, nil
}

func (s *Service) GetFollowUp(ctx context.Context, id string) (FollowUpView, error) {
	var row domain.FollowUpItem
	if err := s.db.WithContext(ctx).First(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FollowUpView{}, ErrNotFound
		}
		return FollowUpView{}, fmt.Errorf("get follow-up item: %w", err)
	}
	return followUpView(row), nil
}

func (s *Service) CreateFollowUp(ctx context.Context, input FollowUpInput) (FollowUpView, error) {
	input = normalizeFollowUpInput(input)
	if err := validateFollowUpInput(input, true); err != nil {
		return FollowUpView{}, err
	}
	if err := s.requireOpenWeek(ctx, input.Quarter, input.Week); err != nil {
		return FollowUpView{}, err
	}
	var existing domain.FollowUpItem
	err := s.db.WithContext(ctx).First(&existing, "id = ?", input.ID).Error
	if err == nil {
		if sameFollowUp(existing, input) {
			return followUpView(existing), nil
		}
		return FollowUpView{}, fmt.Errorf("follow-up id %s already exists with different content", input.ID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return FollowUpView{}, fmt.Errorf("check follow-up id: %w", err)
	}
	now := time.Now().UTC()
	row := domain.FollowUpItem{
		ID: input.ID, Quarter: input.Quarter, Week: input.Week, Topic: input.Topic,
		Owners: input.Owners, Status: input.Status, AssignDate: input.AssignDate, Update: input.Update,
		SourceKey: input.SourceKey, SourcePayload: input.SourcePayload, SortOrder: input.SortOrder,
		CreatedBy: input.UpdatedBy, UpdatedBy: input.UpdatedBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return FollowUpView{}, fmt.Errorf("create follow-up item: %w", err)
	}
	return followUpView(row), nil
}

func (s *Service) UpdateFollowUp(ctx context.Context, id string, input FollowUpInput) (FollowUpView, error) {
	id = strings.TrimSpace(id)
	input = normalizeFollowUpInput(input)
	if id == "" {
		return FollowUpView{}, fmt.Errorf("follow_up_id is required")
	}
	if input.ID != "" && input.ID != id {
		return FollowUpView{}, fmt.Errorf("follow-up id cannot be changed")
	}
	input.ID = id
	if err := validateFollowUpInput(input, false); err != nil {
		return FollowUpView{}, err
	}
	var current domain.FollowUpItem
	if err := s.db.WithContext(ctx).First(&current, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FollowUpView{}, ErrNotFound
		}
		return FollowUpView{}, fmt.Errorf("get follow-up before update: %w", err)
	}
	if input.Quarter != current.Quarter || input.Week != current.Week {
		return FollowUpView{}, fmt.Errorf("follow-up scope cannot be changed")
	}
	if err := s.requireOpenWeek(ctx, current.Quarter, current.Week); err != nil {
		return FollowUpView{}, err
	}
	ownersJSON, err := encodeJSONColumn("follow-up owners", input.Owners)
	if err != nil {
		return FollowUpView{}, err
	}
	result := s.db.WithContext(ctx).Model(&domain.FollowUpItem{}).
		Where("id = ? AND version = ?", id, input.ExpectedVersion).
		Updates(map[string]any{
			"topic": input.Topic, "owners": ownersJSON, "status": input.Status,
			"assign_date": input.AssignDate, "update": input.Update, "source_key": input.SourceKey,
			"source_payload": input.SourcePayload, "sort_order": input.SortOrder, "updated_by": input.UpdatedBy,
			"updated_at": time.Now().UTC(), "version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return FollowUpView{}, fmt.Errorf("update follow-up item: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return FollowUpView{}, followUpWriteConflict(s.db.WithContext(ctx), id)
	}
	return s.GetFollowUp(ctx, id)
}

func (s *Service) DeleteFollowUp(ctx context.Context, id string, input DeleteFollowUpInput) error {
	id = strings.TrimSpace(id)
	if id == "" || input.ExpectedVersion < 0 {
		return fmt.Errorf("follow_up_id is required and expected_version must be non-negative")
	}
	result := s.db.WithContext(ctx).Where("id = ? AND version = ?", id, input.ExpectedVersion).Delete(&domain.FollowUpItem{})
	if result.Error != nil {
		return fmt.Errorf("delete follow-up item: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return followUpWriteConflict(s.db.WithContext(ctx), id)
	}
	return nil
}

func normalizeFollowUpInput(input FollowUpInput) FollowUpInput {
	input.ID, input.Quarter, input.Week = strings.TrimSpace(input.ID), strings.TrimSpace(input.Quarter), strings.TrimSpace(input.Week)
	input.Topic, input.AssignDate, input.Update = strings.TrimSpace(input.Topic), strings.TrimSpace(input.AssignDate), strings.TrimSpace(input.Update)
	input.SourceKey, input.UpdatedBy = strings.TrimSpace(input.SourceKey), strings.TrimSpace(input.UpdatedBy)
	if len(input.SourcePayload) == 0 || string(input.SourcePayload) == "null" {
		input.SourcePayload = datatypes.JSON(`{}`)
	}
	owners := make([]domain.FollowUpOwner, 0, len(input.Owners))
	seen := make(map[string]struct{}, len(input.Owners))
	for _, owner := range input.Owners {
		owner.OpenID, owner.Name = strings.TrimSpace(owner.OpenID), strings.TrimSpace(owner.Name)
		key := owner.OpenID
		if key == "" {
			key = "name:" + owner.Name
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		owners = append(owners, owner)
	}
	input.Owners = owners
	return input
}

func validateFollowUpInput(input FollowUpInput, creating bool) error {
	if input.ExpectedVersion < 0 || (creating && input.ExpectedVersion != 0) {
		return fmt.Errorf("expected_version must be zero when creating and non-negative when updating")
	}
	if input.ID == "" || !quarterPattern.MatchString(input.Quarter) || !weekPattern.MatchString(input.Week) {
		return fmt.Errorf("id, quarter YYYY-Qn and week YYYY-Www are required")
	}
	if input.Topic == "" || !domain.ValidFollowUpStatus(input.Status) {
		return fmt.Errorf("topic and status not_started, in_progress, done or abandoned are required")
	}
	if input.AssignDate != "" {
		parsed, err := time.Parse("2006-01-02", input.AssignDate)
		if err != nil || parsed.Format("2006-01-02") != input.AssignDate {
			return fmt.Errorf("assign_date must use YYYY-MM-DD")
		}
	}
	for _, owner := range input.Owners {
		if owner.OpenID == "" || owner.Name == "" {
			return fmt.Errorf("every follow-up owner requires open_id and name")
		}
	}
	if !json.Valid(input.SourcePayload) {
		return fmt.Errorf("source_payload must be valid JSON")
	}
	return nil
}

func sameFollowUp(row domain.FollowUpItem, input FollowUpInput) bool {
	return row.Quarter == input.Quarter && row.Week == input.Week && row.Topic == input.Topic &&
		reflect.DeepEqual(row.Owners, input.Owners) && row.Status == input.Status && row.AssignDate == input.AssignDate &&
		row.Update == input.Update && row.SourceKey == input.SourceKey && string(row.SourcePayload) == string(input.SourcePayload) &&
		row.SortOrder == input.SortOrder
}

func followUpView(row domain.FollowUpItem) FollowUpView {
	owners := row.Owners
	if owners == nil {
		owners = []domain.FollowUpOwner{}
	}
	source := row.SourcePayload
	if len(source) == 0 || string(source) == "null" {
		source = datatypes.JSON(`{}`)
	}
	return FollowUpView{
		ID: row.ID, Quarter: row.Quarter, Week: row.Week, Version: row.Version, Topic: row.Topic,
		Owners: owners, Status: row.Status, AssignDate: row.AssignDate, Update: row.Update,
		SourceKey: row.SourceKey, SourcePayload: source, SortOrder: row.SortOrder,
		CreatedBy: row.CreatedBy, UpdatedBy: row.UpdatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func followUpWriteConflict(db *gorm.DB, id string) error {
	var count int64
	if err := db.Model(&domain.FollowUpItem{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("check follow-up conflict: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return ErrConflict
}
