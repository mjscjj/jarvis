// Package observe reads the observations M3 and M5 record: facts worth keeping
// that ask nothing of the principal. Writing them belongs to the stage that saw
// them, so this package only lists and prunes.
package observe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

var (
	ErrInvalidInput = errors.New("invalid observation query")
	ErrNotFound     = errors.New("observation not found")
)

type Filter struct {
	Producer  string
	ProjectID *uint64
	Keyword   string
	Page      int
	PageSize  int
}

type View struct {
	ID               uint64    `json:"id"`
	Producer         string    `json:"producer"`
	Subject          string    `json:"subject"`
	Content          string    `json:"content"`
	ProjectID        *uint64   `json:"project_id"`
	ProjectName      *string   `json:"project_name"`
	GroupID          *uint64   `json:"group_id"`
	GroupName        *string   `json:"group_name"`
	SourceRunID      *uint64   `json:"source_run_id"`
	SourceMessageIDs []string  `json:"source_message_ids"`
	SourceQuote      string    `json:"source_quote"`
	ObservedAt       time.Time `json:"observed_at"`
	CreatedAt        time.Time `json:"created_at"`
}

type List struct {
	Items    []View `json:"items"`
	Total    int64  `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

type Service interface {
	List(context.Context, Filter) (*List, error)
	Delete(context.Context, uint64) error
}

type GORMService struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) (*GORMService, error) {
	if db == nil {
		return nil, fmt.Errorf("observe service db is nil")
	}
	return &GORMService{db: db}, nil
}

func (s *GORMService) List(ctx context.Context, filter Filter) (*List, error) {
	producer, err := validateFilter(filter)
	if err != nil {
		return nil, err
	}

	query := s.db.WithContext(ctx).Model(&domain.Observation{})
	if producer != "" {
		query = query.Where("producer = ?", producer)
	}
	if filter.ProjectID != nil {
		query = query.Where("project_id = ?", *filter.ProjectID)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("subject LIKE ? OR content LIKE ?", like, like)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count observations: %w", err)
	}
	var rows []domain.Observation
	if err := query.Order("observed_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}

	names, err := s.resolveNames(ctx, rows)
	if err != nil {
		return nil, err
	}
	items := make([]View, len(rows))
	for i := range rows {
		items[i] = toView(&rows[i], names)
	}
	return &List{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

// validateFilter checks the query and returns the normalized producer.
func validateFilter(filter Filter) (string, error) {
	if filter.Page <= 0 || filter.PageSize <= 0 || filter.PageSize > 100 {
		return "", fmt.Errorf("%w: page must be positive and page_size must be between 1 and 100", ErrInvalidInput)
	}
	producer := strings.TrimSpace(filter.Producer)
	if producer != "" && producer != domain.ObservationProducerM3 && producer != domain.ObservationProducerM5 {
		return "", fmt.Errorf("%w: unsupported producer %q", ErrInvalidInput, producer)
	}
	if filter.ProjectID != nil && *filter.ProjectID == 0 {
		return "", fmt.Errorf("%w: project_id must be positive", ErrInvalidInput)
	}
	return producer, nil
}

func (s *GORMService) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return fmt.Errorf("%w: id must be positive", ErrInvalidInput)
	}
	result := s.db.WithContext(ctx).Delete(&domain.Observation{}, id)
	if result.Error != nil {
		return fmt.Errorf("delete observation id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

// labels holds the display names for the projects and groups on this page.
type labels struct {
	projects map[uint64]string
	groups   map[uint64]string
}

// resolveNames batches the project/group lookups for one page so rendering does
// not fire a query per row.
func (s *GORMService) resolveNames(ctx context.Context, rows []domain.Observation) (labels, error) {
	resolved := labels{projects: map[uint64]string{}, groups: map[uint64]string{}}
	projectIDs := make([]uint64, 0, len(rows))
	groupIDs := make([]uint64, 0, len(rows))
	for i := range rows {
		if rows[i].ProjectID != nil {
			projectIDs = append(projectIDs, *rows[i].ProjectID)
		}
		if rows[i].GroupID != nil {
			groupIDs = append(groupIDs, *rows[i].GroupID)
		}
	}
	if len(projectIDs) > 0 {
		var projects []domain.Project
		if err := s.db.WithContext(ctx).Select("id", "name").Where("id IN ?", projectIDs).Find(&projects).Error; err != nil {
			return resolved, fmt.Errorf("load observation projects: %w", err)
		}
		for i := range projects {
			resolved.projects[projects[i].ID] = projects[i].Name
		}
	}
	if len(groupIDs) > 0 {
		var groups []domain.Group
		if err := s.db.WithContext(ctx).Select("id", "name", "chat_id").Where("id IN ?", groupIDs).Find(&groups).Error; err != nil {
			return resolved, fmt.Errorf("load observation groups: %w", err)
		}
		for i := range groups {
			name := groups[i].ChatID
			if groups[i].Name != nil && strings.TrimSpace(*groups[i].Name) != "" {
				name = strings.TrimSpace(*groups[i].Name)
			}
			resolved.groups[groups[i].ID] = name
		}
	}
	return resolved, nil
}

func toView(row *domain.Observation, names labels) View {
	view := View{
		ID: row.ID, Producer: row.Producer, Subject: row.Subject, Content: row.Content,
		ProjectID: row.ProjectID, GroupID: row.GroupID, SourceRunID: row.SourceRunID,
		SourceQuote: row.SourceQuote, ObservedAt: row.ObservedAt, CreatedAt: row.CreatedAt,
		SourceMessageIDs: decodeMessageIDs(row.SourceMessageIDs),
	}
	if row.ProjectID != nil {
		if name, ok := names.projects[*row.ProjectID]; ok {
			view.ProjectName = &name
		}
	}
	if row.GroupID != nil {
		if name, ok := names.groups[*row.GroupID]; ok {
			view.GroupName = &name
		}
	}
	return view
}

// decodeMessageIDs tolerates an unreadable citation list: the observation text
// is still worth showing, and this column is display-only.
func decodeMessageIDs(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil
	}
	return ids
}
