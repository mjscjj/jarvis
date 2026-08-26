package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
)

// OKRList is the paginated top-level world-model response.
type OKRList struct {
	Items    []OKRView `json:"items"`
	Total    int64     `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

// OKRFilter controls whether closed outcomes are included.
type OKRFilter struct {
	ListFilter
	IncludeClosed bool
}

// OKRService owns the OKR row and its structured links to an owner and
// projects. Progress prose is owned by PageService and progress.Fact.
type OKRService struct {
	db     *gorm.DB
	events *progress.Service
	now    func() time.Time
}

func NewOKRService(db *gorm.DB) (*OKRService, error) {
	if db == nil {
		return nil, fmt.Errorf("okr service db is nil")
	}
	events, err := progress.NewService(db)
	if err != nil {
		return nil, err
	}
	return &OKRService{db: db, events: events, now: time.Now}, nil
}

func (s *OKRService) Create(ctx context.Context, in OKRInput) (*OKRView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := s.requireOwner(ctx, in.OwnerPersonID); err != nil {
		return nil, err
	}
	okr := domain.OKR{
		Title: in.Title, Cycle: in.Cycle, Status: in.Status, OwnerPersonID: in.OwnerPersonID,
	}
	if err := s.db.WithContext(ctx).Create(&okr).Error; err != nil {
		return nil, fmt.Errorf("create okr: %w", err)
	}
	occurredAt := okr.CreatedAt
	if occurredAt.IsZero() {
		occurredAt = s.now().UTC()
	}
	if _, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: "okr", SubjectID: okr.ID,
		Description: fmt.Sprintf("创建 %s 的 OKR“%s”，当前状态为“%s”。", okr.Cycle, okr.Title, okr.Status),
		OccurredAt:  &occurredAt, SourceKind: &factSourceBackground,
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, okr.ID)
}

func (s *OKRService) Get(ctx context.Context, id uint64) (*OKRView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("okr id must be positive"))
	}
	var okr domain.OKR
	err := s.hierarchyQuery(ctx).Where("okr.id = ?", id).Take(&okr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get okr id=%d: %w", id, err)
	}
	view := toOKRView(&okr)
	return &view, nil
}

func (s *OKRService) Update(ctx context.Context, id uint64, in OKRInput) (*OKRView, error) {
	if id == 0 {
		return nil, invalid(fmt.Errorf("okr id must be positive"))
	}
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	if err := s.requireOwner(ctx, in.OwnerPersonID); err != nil {
		return nil, err
	}
	var before domain.OKR
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&before).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load okr id=%d before update: %w", id, err)
	}
	if before.Title == in.Title && before.Cycle == in.Cycle && before.Status == in.Status &&
		sameOptionalUint64(before.OwnerPersonID, in.OwnerPersonID) {
		return s.Get(ctx, id)
	}
	result := s.db.WithContext(ctx).Model(&domain.OKR{}).Where("id = ?", id).Updates(map[string]any{
		"title": in.Title, "cycle": in.Cycle, "status": in.Status, "owner_person_id": in.OwnerPersonID,
	})
	if result.Error != nil {
		return nil, fmt.Errorf("update okr id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return nil, fmt.Errorf("update okr id=%d affected %d rows", id, result.RowsAffected)
	}
	if before.Status != in.Status {
		now := s.now().UTC()
		if _, err := s.events.AppendFact(ctx, progress.FactInput{
			SubjectType: "okr", SubjectID: id,
			Description: fmt.Sprintf("OKR 状态从“%s”调整为“%s”。", before.Status, in.Status),
			OccurredAt:  &now, SourceKind: &factSourceBackground,
		}); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

// Delete closes an OKR without deleting its projects, matters, or history.
func (s *OKRService) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return invalid(fmt.Errorf("okr id must be positive"))
	}
	var okr domain.OKR
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&okr).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("load okr id=%d before close: %w", id, err)
	}
	if okr.ClosedAt != nil {
		return invalid(fmt.Errorf("okr id=%d is already closed", id))
	}
	now := s.now().UTC()
	result := s.db.WithContext(ctx).Model(&domain.OKR{}).Where("id = ? AND closed_at IS NULL", id).Update("closed_at", now)
	if result.Error != nil {
		return fmt.Errorf("close okr id=%d: %w", id, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("close okr id=%d affected %d rows", id, result.RowsAffected)
	}
	if _, err := s.events.AppendFact(ctx, progress.FactInput{
		SubjectType: "okr", SubjectID: id, Description: fmt.Sprintf("OKR“%s”已闭环。", okr.Title),
		OccurredAt: &now, SourceKind: &factSourceBackground,
	}); err != nil {
		return err
	}
	return nil
}

func (s *OKRService) List(ctx context.Context, filter OKRFilter) (*OKRList, error) {
	if err := filter.ListFilter.validate(); err != nil {
		return nil, invalid(err)
	}
	query := s.db.WithContext(ctx).Model(&domain.OKR{})
	if !filter.IncludeClosed {
		query = query.Where("closed_at IS NULL")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count okrs: %w", err)
	}
	items := make([]domain.OKR, 0, filter.PageSize)
	if total > 0 {
		query = s.hierarchyQuery(ctx)
		if !filter.IncludeClosed {
			query = query.Where("okr.closed_at IS NULL")
		}
		if err := query.Order("okr.cycle DESC, okr.id DESC").Limit(filter.PageSize).Offset(filter.offset()).Find(&items).Error; err != nil {
			return nil, fmt.Errorf("list okrs: %w", err)
		}
	}
	return &OKRList{Items: toOKRViews(items), Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s *OKRService) hierarchyQuery(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).
		Preload("Owner").
		Preload("Projects", func(db *gorm.DB) *gorm.DB { return db.Order("priority ASC, id ASC") }).
		Preload("Projects.KeyMatters", "closed_at IS NULL")
}

func (s *OKRService) requireOwner(ctx context.Context, ownerID *uint64) error {
	if ownerID == nil {
		return nil
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&domain.Person{}).Where("id = ?", *ownerID).Count(&count).Error; err != nil {
		return fmt.Errorf("verify okr owner person id=%d: %w", *ownerID, err)
	}
	if count != 1 {
		return invalid(fmt.Errorf("okr owner_person_id=%d does not exist", *ownerID))
	}
	return nil
}
