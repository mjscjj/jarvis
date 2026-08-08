package progress

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

const (
	RollupStateFresh   = "fresh"
	RollupStateStale   = "stale"
	RollupStateMissing = "missing"
)

type FactTimelineFilter struct {
	Days        int
	Location    *time.Location
	SubjectType string
	SubjectID   uint64
}

type FactSearchFilter struct {
	Query       string
	From        *time.Time
	Until       *time.Time
	SubjectType string
	SubjectID   uint64
	SourceKind  string
	Layer       string
	Page        int
	PageSize    int
}

type LabeledFactView struct {
	FactView
	SubjectLabel string `json:"subject_label"`
}

type FactSubjectDayView struct {
	SubjectType      string           `json:"subject_type"`
	SubjectID        uint64           `json:"subject_id"`
	SubjectLabel     string           `json:"subject_label"`
	Rollup           *LabeledFactView `json:"rollup"`
	RollupState      string           `json:"rollup_state"`
	DetailCount      int              `json:"detail_count"`
	LateDetailCount  int              `json:"late_detail_count"`
	LatestOccurredAt time.Time        `json:"latest_occurred_at"`
}

type FactTimelineDayView struct {
	Date        string               `json:"date"`
	IsToday     bool                 `json:"is_today"`
	DetailCount int                  `json:"detail_count"`
	Details     []LabeledFactView    `json:"details"`
	Subjects    []FactSubjectDayView `json:"subjects"`
}

type FactTimelineView struct {
	Timezone string                `json:"timezone"`
	Days     []FactTimelineDayView `json:"days"`
}

type FactSearchView struct {
	Items    []LabeledFactView `json:"items"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

type FactQueryService interface {
	FactTimeline(context.Context, FactTimelineFilter) (FactTimelineView, error)
	SearchFacts(context.Context, FactSearchFilter) (FactSearchView, error)
}

type factSubjectKey struct {
	Type string
	ID   uint64
}

type timelineSubjectAccumulator struct {
	details []domain.Fact
	rollups []domain.Fact
}

func (s *Service) FactTimeline(ctx context.Context, filter FactTimelineFilter) (FactTimelineView, error) {
	if filter.Location == nil {
		return FactTimelineView{}, fmt.Errorf("%w: fact timeline location is nil", ErrInvalidInput)
	}
	if filter.Days <= 0 || filter.Days > 31 {
		return FactTimelineView{}, fmt.Errorf("%w: days must be between 1 and 31", ErrInvalidInput)
	}
	filter.SubjectType = strings.TrimSpace(strings.ToLower(filter.SubjectType))
	if (filter.SubjectType == "") != (filter.SubjectID == 0) {
		return FactTimelineView{}, fmt.Errorf("%w: subject_type and subject_id must be provided together", ErrInvalidInput)
	}

	now := s.now().In(filter.Location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, filter.Location)
	from := today.AddDate(0, 0, -(filter.Days - 1))
	until := today.AddDate(0, 0, 1)
	query := s.db.WithContext(ctx).Where("occurred_at >= ? AND occurred_at < ?", from.UTC(), until.UTC())
	if filter.SubjectType != "" {
		query = query.Where("subject_type = ? AND subject_id = ?", filter.SubjectType, filter.SubjectID)
	}
	var rows []domain.Fact
	if err := query.Order("occurred_at DESC, id DESC").Find(&rows).Error; err != nil {
		return FactTimelineView{}, fmt.Errorf("load fact timeline: %w", err)
	}
	labels, err := s.factSubjectLabels(ctx, rows)
	if err != nil {
		return FactTimelineView{}, err
	}

	result := FactTimelineView{Timezone: filter.Location.String(), Days: make([]FactTimelineDayView, filter.Days)}
	daySubjects := make([]map[factSubjectKey]*timelineSubjectAccumulator, filter.Days)
	dayIndexByDate := make(map[string]int, filter.Days)
	for index := 0; index < filter.Days; index++ {
		day := today.AddDate(0, 0, -index)
		result.Days[index] = FactTimelineDayView{Date: day.Format("2006-01-02"), IsToday: index == 0, Details: []LabeledFactView{}, Subjects: []FactSubjectDayView{}}
		daySubjects[index] = make(map[factSubjectKey]*timelineSubjectAccumulator)
		dayIndexByDate[day.Format("2006-01-02")] = index
	}
	for i := range rows {
		row := rows[i]
		rowDay := row.OccurredAt.In(filter.Location)
		dayIndex, exists := dayIndexByDate[rowDay.Format("2006-01-02")]
		if !exists {
			continue
		}
		key := factSubjectKey{Type: row.SubjectType, ID: row.SubjectID}
		if dayIndex == 0 && !isRollupFact(row) {
			result.Days[0].Details = append(result.Days[0].Details, labeledFactView(row, labels[key]))
			result.Days[0].DetailCount++
			continue
		}
		bucket := daySubjects[dayIndex][key]
		if bucket == nil {
			bucket = &timelineSubjectAccumulator{}
			daySubjects[dayIndex][key] = bucket
		}
		if isRollupFact(row) {
			bucket.rollups = append(bucket.rollups, row)
		} else {
			bucket.details = append(bucket.details, row)
			result.Days[dayIndex].DetailCount++
		}
	}

	for dayIndex := 1; dayIndex < filter.Days; dayIndex++ {
		for key, bucket := range daySubjects[dayIndex] {
			entry := FactSubjectDayView{SubjectType: key.Type, SubjectID: key.ID, SubjectLabel: labels[key], DetailCount: len(bucket.details), RollupState: RollupStateMissing}
			if len(bucket.details) > 0 {
				entry.LatestOccurredAt = bucket.details[0].OccurredAt
			}
			if len(bucket.rollups) > 0 {
				rollup := labeledFactView(bucket.rollups[0], labels[key])
				entry.Rollup = &rollup
				entry.RollupState = RollupStateFresh
				for _, detail := range bucket.details {
					if detail.CreatedAt.After(bucket.rollups[0].CreatedAt) {
						entry.LateDetailCount++
					}
				}
				if entry.LateDetailCount > 0 {
					entry.RollupState = RollupStateStale
				}
			}
			result.Days[dayIndex].Subjects = append(result.Days[dayIndex].Subjects, entry)
		}
		sort.Slice(result.Days[dayIndex].Subjects, func(i, j int) bool {
			left, right := result.Days[dayIndex].Subjects[i], result.Days[dayIndex].Subjects[j]
			if left.LatestOccurredAt.Equal(right.LatestOccurredAt) {
				return left.SubjectLabel < right.SubjectLabel
			}
			return left.LatestOccurredAt.After(right.LatestOccurredAt)
		})
	}
	return result, nil
}

func (s *Service) SearchFacts(ctx context.Context, filter FactSearchFilter) (FactSearchView, error) {
	filter.SubjectType = strings.TrimSpace(strings.ToLower(filter.SubjectType))
	filter.SourceKind = strings.TrimSpace(filter.SourceKind)
	filter.Layer = strings.TrimSpace(strings.ToLower(filter.Layer))
	if filter.Layer == "" {
		filter.Layer = "all"
	}
	if filter.Layer != "all" && filter.Layer != "detail" && filter.Layer != "rollup" {
		return FactSearchView{}, fmt.Errorf("%w: layer must be all, detail or rollup", ErrInvalidInput)
	}
	if filter.Page <= 0 || filter.PageSize <= 0 || filter.PageSize > 200 {
		return FactSearchView{}, fmt.Errorf("%w: page must be positive and page_size must be between 1 and 200", ErrInvalidInput)
	}
	if filter.SubjectID != 0 && filter.SubjectType == "" {
		return FactSearchView{}, fmt.Errorf("%w: subject_type is required with subject_id", ErrInvalidInput)
	}

	query := s.db.WithContext(ctx).Model(&domain.Fact{})
	if filter.From != nil {
		query = query.Where("occurred_at >= ?", filter.From.UTC())
	}
	if filter.Until != nil {
		query = query.Where("occurred_at < ?", filter.Until.UTC())
	}
	if filter.SubjectType != "" {
		query = query.Where("subject_type = ?", filter.SubjectType)
	}
	if filter.SubjectID != 0 {
		query = query.Where("subject_id = ?", filter.SubjectID)
	}
	if filter.SourceKind != "" {
		query = query.Where("source_kind = ?", filter.SourceKind)
	}
	if filter.Layer == "detail" {
		query = query.Where("source_kind IS NULL OR source_kind <> ?", FactSourceRollup)
	} else if filter.Layer == "rollup" {
		query = query.Where("source_kind = ?", FactSourceRollup)
	}
	var rows []domain.Fact
	if err := query.Order("occurred_at DESC, id DESC").Find(&rows).Error; err != nil {
		return FactSearchView{}, fmt.Errorf("search facts: %w", err)
	}
	labels, err := s.factSubjectLabels(ctx, rows)
	if err != nil {
		return FactSearchView{}, err
	}
	needle := strings.ToLower(strings.TrimSpace(filter.Query))
	matches := make([]LabeledFactView, 0, len(rows))
	for _, row := range rows {
		key := factSubjectKey{Type: row.SubjectType, ID: row.SubjectID}
		view := labeledFactView(row, labels[key])
		if needle != "" {
			haystack := strings.ToLower(view.Description + "\n" + view.SubjectLabel + "\n" + fmt.Sprintf("%s/%d", view.SubjectType, view.SubjectID))
			if !strings.Contains(haystack, needle) {
				continue
			}
		}
		matches = append(matches, view)
	}
	start := (filter.Page - 1) * filter.PageSize
	end := min(start+filter.PageSize, len(matches))
	items := []LabeledFactView{}
	if start < len(matches) {
		items = matches[start:end]
	}
	return FactSearchView{Items: items, Total: len(matches), Page: filter.Page, PageSize: filter.PageSize}, nil
}

func isRollupFact(fact domain.Fact) bool {
	return fact.SourceKind != nil && *fact.SourceKind == FactSourceRollup
}

func labeledFactView(fact domain.Fact, label string) LabeledFactView {
	return LabeledFactView{FactView: factView(&fact), SubjectLabel: label}
}

func (s *Service) factSubjectLabels(ctx context.Context, facts []domain.Fact) (map[factSubjectKey]string, error) {
	labels := make(map[factSubjectKey]string)
	for _, fact := range facts {
		key := factSubjectKey{Type: fact.SubjectType, ID: fact.SubjectID}
		if _, exists := labels[key]; exists {
			continue
		}
		label, err := s.factSubjectLabel(ctx, key)
		if err != nil {
			return nil, err
		}
		labels[key] = label
	}
	return labels, nil
}

func (s *Service) factSubjectLabel(ctx context.Context, key factSubjectKey) (string, error) {
	fallback := fmt.Sprintf("%s/%d", key.Type, key.ID)
	db := s.db.WithContext(ctx)
	var err error
	switch key.Type {
	case "project":
		var row domain.Project
		err = db.Select("id", "name").First(&row, key.ID).Error
		if err == nil {
			return row.Name, nil
		}
	case "key_matter":
		var row domain.KeyMatter
		err = db.Select("id", "title").First(&row, key.ID).Error
		if err == nil {
			return row.Title, nil
		}
	case "group":
		var row domain.Group
		err = db.Select("id", "name", "chat_id").First(&row, key.ID).Error
		if err == nil {
			if row.Name != nil && strings.TrimSpace(*row.Name) != "" {
				return *row.Name, nil
			}
			if strings.TrimSpace(row.ChatID) != "" {
				return row.ChatID, nil
			}
			return fallback, nil
		}
	case "person":
		var row domain.Person
		err = db.Select("id", "name").First(&row, key.ID).Error
		if err == nil {
			return row.Name, nil
		}
	case "task":
		var row domain.Task
		err = db.Select("id", "title").First(&row, key.ID).Error
		if err == nil {
			return row.Title, nil
		}
	case "todo":
		var row domain.Todo
		err = db.Select("id", "title").First(&row, key.ID).Error
		if err == nil {
			return row.Title, nil
		}
	case "resource":
		var row domain.Resource
		err = db.Select("id", "name").First(&row, key.ID).Error
		if err == nil && row.Name != nil && strings.TrimSpace(*row.Name) != "" {
			return *row.Name, nil
		}
		if err == nil {
			return fallback, nil
		}
	case "managed_resource":
		var row domain.ManagedResource
		err = db.Select("id", "title").First(&row, key.ID).Error
		if err == nil {
			return row.Title, nil
		}
	case "principal":
		var row domain.PrincipalProfile
		err = db.Select("id", "name").First(&row, key.ID).Error
		if err == nil {
			return row.Name, nil
		}
	default:
		return fallback, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fallback, nil
	}
	return "", fmt.Errorf("load fact subject label %s: %w", fallback, err)
}
