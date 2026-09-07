package background

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var relationToken = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type RelationInput struct {
	SourceType   string         `json:"source_type"`
	SourceID     string         `json:"source_id"`
	RelationType string         `json:"relation_type"`
	TargetType   string         `json:"target_type"`
	TargetID     string         `json:"target_id"`
	Evidence     datatypes.JSON `json:"evidence"`
	Confidence   *float64       `json:"confidence"`
	ConfirmedAt  *time.Time     `json:"confirmed_at"`
}

type RelationFilter struct {
	SourceType   string
	SourceID     string
	RelationType string
	TargetType   string
	TargetID     string
	NodeType     string
	NodeID       string
	NodeTypes    []string
	Cursor       uint64
	Limit        int
}

type RelationPage struct {
	Items      []domain.EntityRelation `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type RelationService struct{ db *gorm.DB }

func NewRelationService(db *gorm.DB) (*RelationService, error) {
	if db == nil {
		return nil, fmt.Errorf("relation service db is nil")
	}
	return &RelationService{db: db}, nil
}

func (s *RelationService) Upsert(ctx context.Context, input RelationInput) (*domain.EntityRelation, error) {
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.RelationType = strings.TrimSpace(input.RelationType)
	input.TargetType = strings.TrimSpace(input.TargetType)
	input.TargetID = strings.TrimSpace(input.TargetID)
	if !relationToken.MatchString(input.SourceType) || !relationToken.MatchString(input.RelationType) || !relationToken.MatchString(input.TargetType) {
		return nil, invalid(fmt.Errorf("relation types must use lowercase letters, digits, underscores, or hyphens"))
	}
	if input.SourceID == "" || input.TargetID == "" {
		return nil, invalid(fmt.Errorf("source_id and target_id are required"))
	}
	if input.Confidence != nil && (*input.Confidence < 0 || *input.Confidence > 1) {
		return nil, invalid(fmt.Errorf("confidence must be between 0 and 1"))
	}
	if len(input.Evidence) == 0 {
		input.Evidence = datatypes.JSON(`{}`)
	}
	var row domain.EntityRelation
	err := s.db.WithContext(ctx).Where(
		"source_type = ? AND source_id = ? AND relation_type = ? AND target_type = ? AND target_id = ?",
		input.SourceType, input.SourceID, input.RelationType, input.TargetType, input.TargetID,
	).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = domain.EntityRelation{
			SourceType: input.SourceType, SourceID: input.SourceID, RelationType: input.RelationType,
			TargetType: input.TargetType, TargetID: input.TargetID, Evidence: input.Evidence,
			Confidence: input.Confidence, ConfirmedAt: input.ConfirmedAt,
		}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, fmt.Errorf("create entity relation: %w", err)
		}
		return &row, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find entity relation: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{
		"evidence": input.Evidence, "confidence": input.Confidence, "confirmed_at": input.ConfirmedAt,
	}).Error; err != nil {
		return nil, fmt.Errorf("update entity relation: %w", err)
	}
	if err := s.db.WithContext(ctx).First(&row, row.ID).Error; err != nil {
		return nil, fmt.Errorf("reload entity relation: %w", err)
	}
	return &row, nil
}

func (s *RelationService) List(ctx context.Context, filter RelationFilter) ([]domain.EntityRelation, error) {
	page, err := s.ListPage(ctx, filter)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (s *RelationService) ListPage(ctx context.Context, filter RelationFilter) (*RelationPage, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		return nil, invalid(fmt.Errorf("limit must be between 1 and 200"))
	}
	filter.SourceType = strings.TrimSpace(filter.SourceType)
	filter.SourceID = strings.TrimSpace(filter.SourceID)
	filter.RelationType = strings.TrimSpace(filter.RelationType)
	filter.TargetType = strings.TrimSpace(filter.TargetType)
	filter.TargetID = strings.TrimSpace(filter.TargetID)
	filter.NodeType = strings.TrimSpace(filter.NodeType)
	filter.NodeID = strings.TrimSpace(filter.NodeID)
	if (filter.NodeType == "") != (filter.NodeID == "") {
		return nil, invalid(fmt.Errorf("node_type and node_id must be provided together"))
	}
	nodeTypes := make([]string, 0, len(filter.NodeTypes))
	for _, value := range filter.NodeTypes {
		value = strings.TrimSpace(value)
		if value == "" || !relationToken.MatchString(value) {
			return nil, invalid(fmt.Errorf("node_types must contain valid type tokens"))
		}
		nodeTypes = append(nodeTypes, value)
	}
	query := s.db.WithContext(ctx).Model(&domain.EntityRelation{})
	for clause, value := range map[string]string{
		"source_type": filter.SourceType, "source_id": filter.SourceID,
		"relation_type": filter.RelationType,
		"target_type":   filter.TargetType, "target_id": filter.TargetID,
	} {
		if value != "" {
			query = query.Where(clause+" = ?", value)
		}
	}
	if filter.NodeType != "" {
		query = query.Where(
			"(source_type = ? AND source_id = ?) OR (target_type = ? AND target_id = ?)",
			filter.NodeType, filter.NodeID, filter.NodeType, filter.NodeID,
		)
	}
	if len(nodeTypes) > 0 {
		query = query.Where("source_type IN ? OR target_type IN ?", nodeTypes, nodeTypes)
	}
	if filter.Cursor != 0 {
		query = query.Where("id < ?", filter.Cursor)
	}
	var rows []domain.EntityRelation
	if err := query.Order("id DESC").Limit(filter.Limit + 1).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list entity relations: %w", err)
	}
	page := &RelationPage{Items: rows}
	if len(rows) > filter.Limit {
		page.Items = rows[:filter.Limit]
		page.NextCursor = fmt.Sprintf("%d", page.Items[len(page.Items)-1].ID)
	}
	if page.Items == nil {
		page.Items = []domain.EntityRelation{}
	}
	return page, nil
}

func (s *RelationService) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return invalid(fmt.Errorf("relation id must be positive"))
	}
	result := s.db.WithContext(ctx).Delete(&domain.EntityRelation{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete entity relation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
