// Package kr implements the KR aggregate, weekly board and optimistic writes.
package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

var (
	ErrNotFound         = errors.New("kr not found")
	ErrConflict         = errors.New("kr version conflict")
	ErrMeegoUnavailable = errors.New("Meego preview unavailable")
	weekPattern         = regexp.MustCompile(`^\d{4}-W(?:0[1-9]|[1-4]\d|5[0-3])$`)
	quarterPattern      = regexp.MustCompile(`^\d{4}-Q[1-4]$`)
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("create kr service: db is nil")
	}
	return &Service{db: db}, nil
}

type Board struct {
	Quarter        string          `json:"quarter"`
	Week           string          `json:"week"`
	PreviousWeek   string          `json:"previous_week,omitempty"`
	AvailableWeeks []string        `json:"available_weeks"`
	Objectives     []ObjectiveView `json:"objectives"`
}

type Scope struct {
	Quarter string `json:"quarter"`
	Week    string `json:"week"`
}

type CoreScope struct {
	Quarter string `json:"quarter"`
}

func (s *Service) LatestCoreScope(ctx context.Context) (CoreScope, error) {
	var quarter string
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Select("quarter").Where("quarter <> ''").Order("quarter DESC").Limit(1).Scan(&quarter).Error; err != nil {
		return CoreScope{}, fmt.Errorf("find latest OKR quarter: %w", err)
	}
	if !quarterPattern.MatchString(quarter) {
		return CoreScope{}, fmt.Errorf("no valid OKR quarter is available")
	}
	return CoreScope{Quarter: quarter}, nil
}

// LatestWeeklyScope returns the newest quarter/week pair that has progress.
// It is the discovery entry point for weekly-report automation.
func (s *Service) LatestWeeklyScope(ctx context.Context) (Scope, error) {
	var scope Scope
	err := s.db.WithContext(ctx).Table("okr_workspace_progress AS progress").
		Select("objective.quarter, progress.week").
		Joins("JOIN okr_workspace_point AS point ON point.id = progress.point_id").
		Joins("JOIN okr_workspace_kr AS kr ON kr.id = point.kr_id").
		Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
		Where("objective.quarter <> '' AND progress.week <> ''").
		Order("objective.quarter DESC, progress.week DESC").Limit(1).Scan(&scope).Error
	if err != nil {
		return Scope{}, fmt.Errorf("find latest weekly report scope: %w", err)
	}
	if !quarterPattern.MatchString(scope.Quarter) || !weekPattern.MatchString(scope.Week) {
		return Scope{}, fmt.Errorf("no valid weekly report scope is available")
	}
	return scope, nil
}

type ReminderPreview struct {
	Quarter     string              `json:"quarter"`
	Week        string              `json:"week"`
	Mode        string              `json:"mode"`
	SendEnabled bool                `json:"send_enabled"`
	Summary     ReminderSummary     `json:"summary"`
	Recipients  []ReminderRecipient `json:"recipients"`
}

type ReminderSummary struct {
	OwnerCount              int `json:"owner_count"`
	NeedsReminderOwnerCount int `json:"needs_reminder_owner_count"`
	DueCount                int `json:"due_count"`
	FilledCount             int `json:"filled_count"`
	MissingCount            int `json:"missing_count"`
}

type ReminderRecipient struct {
	OwnerOpenID   string       `json:"owner_open_id"`
	OwnerName     string       `json:"owner_name"`
	DueCount      int          `json:"due_count"`
	FilledCount   int          `json:"filled_count"`
	MissingCount  int          `json:"missing_count"`
	NeedsReminder bool         `json:"needs_reminder"`
	CanRemind     bool         `json:"can_remind"`
	MissingKRs    []ReminderKR `json:"missing_krs"`
	Message       string       `json:"message"`
}

type ReminderKR struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ObjectiveView struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	KRs   []KRView `json:"krs"`
}

type KRView struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	OwnerOpenID string       `json:"owner_open_id"`
	OwnerName   string       `json:"owner_name"`
	Priority    string       `json:"priority"`
	MetricNote  string       `json:"metric_note"`
	Version     int32        `json:"version"`
	Metrics     []MetricView `json:"metrics"`
	Points      []PointView  `json:"points"`
	Tags        []TagView    `json:"tags"`
	Owners      []OwnerView  `json:"owners"`
}

type OwnerView struct {
	OpenID string `json:"open_id"`
	Name   string `json:"name"`
}

type MetricView struct {
	ID     string            `json:"id"`
	Text   string            `json:"text"`
	Light  domain.Light      `json:"light,omitempty"`
	Images []domain.ImageRef `json:"images"`
}

type PointView struct {
	ID              string           `json:"id"`
	Kind            domain.PointKind `json:"kind"`
	Title           string           `json:"title"`
	MeegoWorkItemID string           `json:"meego_work_item_id"`
	MeegoURL        string           `json:"meego_url"`
	Entries         []ProgressView   `json:"entries"`
	PreviousEntries []ProgressView   `json:"previous_entries"`
}

type MeegoPreview struct {
	PointID      string         `json:"point_id"`
	WorkItemID   string         `json:"work_item_id"`
	URL          string         `json:"url"`
	Mode         string         `json:"mode"`
	WriteEnabled bool           `json:"write_enabled"`
	Local        PreviewContent `json:"local"`
	Remote       PreviewContent `json:"remote"`
	Diff         PreviewDiff    `json:"diff"`
	NeedsReview  bool           `json:"needs_review"`
}

type MeegoBatchPreview struct {
	Quarter      string                  `json:"quarter"`
	Week         string                  `json:"week"`
	Mode         string                  `json:"mode"`
	WriteEnabled bool                    `json:"write_enabled"`
	Summary      MeegoBatchSummary       `json:"summary"`
	Items        []MeegoBatchPreviewItem `json:"items"`
}

type MeegoBatchSummary struct {
	LinkedCount      int `json:"linked_count"`
	ComparedCount    int `json:"compared_count"`
	RiskCount        int `json:"risk_count"`
	NeedsReviewCount int `json:"needs_review_count"`
	ErrorCount       int `json:"error_count"`
	UnsyncedCount    int `json:"unsynced_count"`
	StaleCount       int `json:"stale_count"`
}

type MeegoBatchPreviewItem struct {
	ObjectiveID    string          `json:"objective_id"`
	ObjectiveTitle string          `json:"objective_title"`
	KRID           string          `json:"kr_id"`
	KRTitle        string          `json:"kr_title"`
	KRVersion      int32           `json:"kr_version"`
	OwnerName      string          `json:"owner_name"`
	PointID        string          `json:"point_id"`
	PointTitle     string          `json:"point_title"`
	Risk           bool            `json:"risk"`
	Preview        *MeegoPreview   `json:"preview,omitempty"`
	Sync           *MeegoSyncState `json:"sync,omitempty"`
	Error          string          `json:"error,omitempty"`
}

type MeegoSyncState struct {
	Week          string `json:"week"`
	Status        string `json:"status"`
	LastAttemptAt string `json:"last_attempt_at"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

type MeegoObservationInput struct {
	PointID    string         `json:"point_id"`
	WorkItemID string         `json:"work_item_id"`
	Week       string         `json:"week"`
	Remote     PreviewContent `json:"remote"`
	ObservedAt time.Time      `json:"observed_at"`
	FetchError string         `json:"fetch_error"`
}

type MeegoObservationResult struct {
	Preview *MeegoPreview   `json:"preview,omitempty"`
	Sync    *MeegoSyncState `json:"sync"`
}

type PreviewContent struct {
	Title     string `json:"title,omitempty"`
	Status    string `json:"status"`
	Progress  string `json:"progress"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type PreviewDiff struct {
	StatusChanged   bool `json:"status_changed"`
	ProgressChanged bool `json:"progress_changed"`
}

type ConfirmMeegoProgressInput struct {
	ExpectedVersion int32         `json:"expected_version"`
	Week            string        `json:"week"`
	UpdatedBy       string        `json:"updated_by"`
	MeegoWorkItemID string        `json:"meego_work_item_id"`
	Status          domain.Status `json:"status"`
	Text            string        `json:"text"`
}

type ProgressView struct {
	ID          string            `json:"id"`
	Status      domain.Status     `json:"status"`
	Text        string            `json:"text"`
	Docs        []domain.DocLink  `json:"docs"`
	Images      []domain.ImageRef `json:"images"`
	Source      string            `json:"source"`
	NeedsReview bool              `json:"needs_review"`
}

type TagView struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type ReplaceKRInput struct {
	ExpectedVersion int32        `json:"expected_version"`
	Week            string       `json:"week"`
	UpdatedBy       string       `json:"updated_by"`
	Title           string       `json:"title"`
	OwnerOpenID     string       `json:"owner_open_id"`
	OwnerName       string       `json:"owner_name"`
	Priority        string       `json:"priority"`
	MetricNote      string       `json:"metric_note"`
	Metrics         []MetricView `json:"metrics"`
	Points          []PointView  `json:"points"`
	Tags            []TagView    `json:"tags"`
	Owners          []OwnerView  `json:"owners"`
}

type CreateKRInput struct {
	Title     string `json:"title"`
	OwnerName string `json:"owner_name"`
	Priority  string `json:"priority"`
	CreatedBy string `json:"created_by"`
}

type CreateObjectiveInput struct {
	Quarter string `json:"quarter"`
	Title   string `json:"title"`
}

type DeleteKRInput struct {
	ExpectedVersion int32 `json:"expected_version"`
}

func (s *Service) Board(ctx context.Context, quarter, week string) (Board, error) {
	quarter = strings.TrimSpace(quarter)
	week = strings.TrimSpace(week)
	if quarter == "" || week == "" {
		scope, err := s.LatestWeeklyScope(ctx)
		if err != nil {
			return Board{}, err
		}
		if quarter == "" {
			quarter = scope.Quarter
		}
		if week == "" {
			week = scope.Week
		}
	}
	if !weekPattern.MatchString(week) {
		return Board{}, fmt.Errorf("week must use YYYY-Www")
	}
	var objectives []domain.Objective
	if err := s.db.WithContext(ctx).Where("quarter = ?", quarter).Order("sort_order, id").Find(&objectives).Error; err != nil {
		return Board{}, fmt.Errorf("list objectives: %w", err)
	}
	weeks, previousWeek, err := s.weeks(ctx, quarter, week)
	if err != nil {
		return Board{}, err
	}
	result := Board{Quarter: quarter, Week: week, PreviousWeek: previousWeek, AvailableWeeks: weeks, Objectives: make([]ObjectiveView, 0, len(objectives))}
	for _, objective := range objectives {
		var records []domain.KR
		if err := s.db.WithContext(ctx).Where("objective_id = ?", objective.ID).Order("sort_order, id").Find(&records).Error; err != nil {
			return Board{}, fmt.Errorf("list krs: %w", err)
		}
		view := ObjectiveView{ID: objective.ID, Title: objective.Title, KRs: make([]KRView, 0, len(records))}
		for _, record := range records {
			krView, err := s.loadKR(ctx, record, week, previousWeek)
			if err != nil {
				return Board{}, err
			}
			view.KRs = append(view.KRs, krView)
		}
		result.Objectives = append(result.Objectives, view)
	}
	return result, nil
}

// CoreBoard is the stable OKR projection. It intentionally carries no weekly
// entries or history, even though the compatibility DTO is shared with Board.
func (s *Service) CoreBoard(ctx context.Context, quarter string) (Board, error) {
	quarter = strings.TrimSpace(quarter)
	if quarter == "" {
		scope, err := s.LatestCoreScope(ctx)
		if err != nil {
			return Board{}, err
		}
		quarter = scope.Quarter
	}
	if !quarterPattern.MatchString(quarter) {
		return Board{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	var objectives []domain.Objective
	if err := s.db.WithContext(ctx).Where("quarter = ?", quarter).Order("sort_order, id").Find(&objectives).Error; err != nil {
		return Board{}, fmt.Errorf("list objectives: %w", err)
	}
	result := Board{Quarter: quarter, Week: "", AvailableWeeks: []string{}, Objectives: make([]ObjectiveView, 0, len(objectives))}
	for _, objective := range objectives {
		var records []domain.KR
		if err := s.db.WithContext(ctx).Where("objective_id = ?", objective.ID).Order("sort_order, id").Find(&records).Error; err != nil {
			return Board{}, fmt.Errorf("list krs: %w", err)
		}
		view := ObjectiveView{ID: objective.ID, Title: objective.Title, KRs: make([]KRView, 0, len(records))}
		for _, record := range records {
			krView, err := s.loadKRDefinition(ctx, record)
			if err != nil {
				return Board{}, err
			}
			view.KRs = append(view.KRs, krView)
		}
		result.Objectives = append(result.Objectives, view)
	}
	return result, nil
}

// ReminderPreview derives a reviewable weekly fill summary without performing
// any external send. A KR is filled only when every one of its points has at
// least one non-empty progress entry for the selected week.
func (s *Service) ReminderPreview(ctx context.Context, quarter, week string) (ReminderPreview, error) {
	quarter = strings.TrimSpace(quarter)
	week = strings.TrimSpace(week)
	if quarter == "" || week == "" {
		scope, err := s.LatestWeeklyScope(ctx)
		if err != nil {
			return ReminderPreview{}, err
		}
		if quarter == "" {
			quarter = scope.Quarter
		}
		if week == "" {
			week = scope.Week
		}
	}
	if !weekPattern.MatchString(week) {
		return ReminderPreview{}, fmt.Errorf("week must use YYYY-Www")
	}

	result := ReminderPreview{
		Quarter: quarter, Week: week, Mode: "preview_only", SendEnabled: false,
		Recipients: []ReminderRecipient{},
	}
	var objectiveIDs []string
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Where("quarter = ?", quarter).Pluck("id", &objectiveIDs).Error; err != nil {
		return ReminderPreview{}, fmt.Errorf("list reminder objectives: %w", err)
	}
	if len(objectiveIDs) == 0 {
		return result, nil
	}
	var records []domain.KR
	if err := s.db.WithContext(ctx).Where("objective_id IN ?", objectiveIDs).Order("sort_order, id").Find(&records).Error; err != nil {
		return ReminderPreview{}, fmt.Errorf("list reminder krs: %w", err)
	}
	if len(records) == 0 {
		return result, nil
	}

	krIDs := make([]string, 0, len(records))
	for _, record := range records {
		krIDs = append(krIDs, record.ID)
	}
	var points []domain.KRPoint
	if err := s.db.WithContext(ctx).Where("kr_id IN ?", krIDs).Order("sort_order, id").Find(&points).Error; err != nil {
		return ReminderPreview{}, fmt.Errorf("list reminder points: %w", err)
	}
	pointCountByKR := make(map[string]int, len(records))
	pointOwner := make(map[string]string, len(points))
	pointIDs := make([]string, 0, len(points))
	for _, point := range points {
		pointCountByKR[point.KRID]++
		pointOwner[point.ID] = point.KRID
		pointIDs = append(pointIDs, point.ID)
	}
	filledPointsByKR := make(map[string]map[string]bool, len(records))
	if len(pointIDs) > 0 {
		var progress []domain.KRProgress
		if err := s.db.WithContext(ctx).Where("point_id IN ? AND week = ?", pointIDs, week).Find(&progress).Error; err != nil {
			return ReminderPreview{}, fmt.Errorf("list reminder progress: %w", err)
		}
		for _, entry := range progress {
			if strings.TrimSpace(entry.Text) == "" {
				continue
			}
			krID := pointOwner[entry.PointID]
			if filledPointsByKR[krID] == nil {
				filledPointsByKR[krID] = map[string]bool{}
			}
			filledPointsByKR[krID][entry.PointID] = true
		}
	}

	type recipientAccumulator struct {
		openID  string
		name    string
		due     int
		filled  int
		missing []ReminderKR
	}
	var ownerLinks []domain.KROwner
	if err := s.db.WithContext(ctx).Where("kr_id IN ?", krIDs).Order("sort_order, owner_key, person_id").Find(&ownerLinks).Error; err != nil {
		return ReminderPreview{}, fmt.Errorf("list reminder owners: %w", err)
	}
	ownersByKR := make(map[string][]OwnerView, len(records))
	for _, link := range ownerLinks {
		ownersByKR[link.KRID] = append(ownersByKR[link.KRID], OwnerView{OpenID: link.OpenID, Name: link.Name})
	}
	owners := map[string]*recipientAccumulator{}
	for _, record := range records {
		recordOwners := ownersByKR[record.ID]
		if len(recordOwners) == 0 {
			recordOwners = normalizeOwners(nil, record.OwnerName, record.OwnerOpenID)
		}
		if len(recordOwners) == 0 {
			recordOwners = []OwnerView{{Name: "未分配"}}
		}
		pointCount := pointCountByKR[record.ID]
		filled := pointCount > 0 && len(filledPointsByKR[record.ID]) == pointCount
		for _, recordOwner := range recordOwners {
			openID := strings.TrimSpace(recordOwner.OpenID)
			name := strings.TrimSpace(recordOwner.Name)
			key := openID
			if key == "" {
				key = "name:" + strings.ToLower(name)
			}
			if name == "" {
				name = "未分配"
			}
			if key == "name:" {
				key = "unassigned"
			}
			owner := owners[key]
			if owner == nil {
				owner = &recipientAccumulator{openID: openID, name: name, missing: []ReminderKR{}}
				owners[key] = owner
			}
			owner.due++
			if filled {
				owner.filled++
				continue
			}
			owner.missing = append(owner.missing, ReminderKR{ID: record.ID, Title: record.Title})
		}
	}

	for _, owner := range owners {
		missingCount := len(owner.missing)
		recipient := ReminderRecipient{
			OwnerOpenID: owner.openID, OwnerName: owner.name, DueCount: owner.due,
			FilledCount: owner.filled, MissingCount: missingCount,
			NeedsReminder: missingCount > 0, CanRemind: owner.openID != "" || owner.name != "未分配",
			MissingKRs: owner.missing,
		}
		if missingCount > 0 {
			titles := make([]string, 0, missingCount)
			for _, item := range owner.missing {
				titles = append(titles, item.Title)
			}
			if recipient.CanRemind {
				recipient.Message = fmt.Sprintf("%s，你好。本周（%s）KR 进展还有 %d 条待填写：%s。请在周会前打开 Emily 完成更新，谢谢。", owner.name, week, missingCount, strings.Join(titles, "；"))
			} else {
				recipient.Message = fmt.Sprintf("以下 KR 尚未分配负责人：%s。请先补充负责人，再发起催办。", strings.Join(titles, "；"))
			}
		}
		result.Recipients = append(result.Recipients, recipient)
		result.Summary.DueCount += owner.due
		result.Summary.FilledCount += owner.filled
		result.Summary.MissingCount += missingCount
		if missingCount > 0 {
			result.Summary.NeedsReminderOwnerCount++
		}
	}
	result.Summary.OwnerCount = len(result.Recipients)
	sort.Slice(result.Recipients, func(i, j int) bool {
		left, right := result.Recipients[i], result.Recipients[j]
		if left.NeedsReminder != right.NeedsReminder {
			return left.NeedsReminder
		}
		if left.MissingCount != right.MissingCount {
			return left.MissingCount > right.MissingCount
		}
		return left.OwnerName < right.OwnerName
	})
	return result, nil
}

func (s *Service) GetKR(ctx context.Context, id, week string) (KRView, error) {
	var record domain.KR
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get kr: %w", err)
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ?", record.ObjectiveID).Error; err != nil {
		return KRView{}, fmt.Errorf("get objective for kr: %w", err)
	}
	_, previousWeek, err := s.weeks(ctx, objective.Quarter, week)
	if err != nil {
		return KRView{}, err
	}
	return s.loadKR(ctx, record, week, previousWeek)
}

func (s *Service) GetCoreKR(ctx context.Context, id string) (KRView, error) {
	var record domain.KR
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get kr: %w", err)
	}
	return s.loadKRDefinition(ctx, record)
}

func (s *Service) loadKRDefinition(ctx context.Context, record domain.KR) (KRView, error) {
	var metrics []domain.KRMetric
	var points []domain.KRPoint
	var tags []domain.KRTag
	var owners []domain.KROwner
	if err := s.db.WithContext(ctx).Where("kr_id = ?", record.ID).Order("sort_order, id").Find(&metrics).Error; err != nil {
		return KRView{}, fmt.Errorf("list metrics: %w", err)
	}
	if err := s.db.WithContext(ctx).Where("kr_id = ?", record.ID).Order("sort_order, id").Find(&points).Error; err != nil {
		return KRView{}, fmt.Errorf("list points: %w", err)
	}
	if err := s.db.WithContext(ctx).Where("kr_id = ?", record.ID).Order("type, value").Find(&tags).Error; err != nil {
		return KRView{}, fmt.Errorf("list tags: %w", err)
	}
	if err := s.db.WithContext(ctx).Where("kr_id = ?", record.ID).Order("sort_order, owner_key, person_id").Find(&owners).Error; err != nil {
		return KRView{}, fmt.Errorf("list owners: %w", err)
	}
	view := KRView{ID: record.ID, Title: record.Title, OwnerOpenID: record.OwnerOpenID, OwnerName: record.OwnerName, Priority: record.Priority, MetricNote: record.MetricNote, Version: record.Version, Metrics: []MetricView{}, Points: []PointView{}, Tags: []TagView{}, Owners: []OwnerView{}}
	for _, owner := range owners {
		view.Owners = append(view.Owners, OwnerView{OpenID: owner.OpenID, Name: owner.Name})
	}
	if len(view.Owners) == 0 && strings.TrimSpace(record.OwnerName) != "" {
		names := strings.FieldsFunc(record.OwnerName, func(r rune) bool { return r == '、' || r == ',' || r == '，' || r == ';' || r == '；' })
		for i, name := range names {
			owner := OwnerView{Name: strings.TrimSpace(name)}
			if i == 0 {
				owner.OpenID = record.OwnerOpenID
			}
			if owner.Name != "" {
				view.Owners = append(view.Owners, owner)
			}
		}
	}
	for _, metric := range metrics {
		light := metric.Light
		if light == "" {
			light = domain.LightGreen
		}
		view.Metrics = append(view.Metrics, MetricView{ID: metric.ID, Text: metric.Text, Light: light, Images: nonNilImages(metric.Images)})
	}
	for _, point := range points {
		pointView := PointView{ID: point.ID, Kind: point.Kind, Title: point.Title, MeegoWorkItemID: point.MeegoWorkItemID, MeegoURL: point.MeegoURL, Entries: []ProgressView{}, PreviousEntries: []ProgressView{}}
		view.Points = append(view.Points, pointView)
	}
	for _, tag := range tags {
		view.Tags = append(view.Tags, TagView{Type: tag.Type, Value: tag.Value})
	}
	return view, nil
}

func (s *Service) loadKR(ctx context.Context, record domain.KR, week, previousWeek string) (KRView, error) {
	view, err := s.loadKRDefinition(ctx, record)
	if err != nil {
		return KRView{}, err
	}
	for index := range view.Points {
		point := &view.Points[index]
		var progress []domain.KRProgress
		if err := s.db.WithContext(ctx).Where("point_id = ? AND week = ?", point.ID, week).Order("sort_order, id").Find(&progress).Error; err != nil {
			return KRView{}, fmt.Errorf("list progress: %w", err)
		}
		for _, entry := range progress {
			point.Entries = append(point.Entries, ProgressView{ID: entry.ID, Status: entry.Status, Text: entry.Text, Docs: nonNilDocs(entry.Docs), Images: nonNilImages(entry.Images), Source: entry.Source, NeedsReview: entry.NeedsReview})
		}
		if previousWeek == "" {
			continue
		}
		var previous []domain.KRProgress
		if err := s.db.WithContext(ctx).Where("point_id = ? AND week = ?", point.ID, previousWeek).Order("sort_order, id").Find(&previous).Error; err != nil {
			return KRView{}, fmt.Errorf("list previous progress: %w", err)
		}
		for _, entry := range previous {
			point.PreviousEntries = append(point.PreviousEntries, ProgressView{ID: entry.ID, Status: entry.Status, Text: entry.Text, Docs: nonNilDocs(entry.Docs), Images: nonNilImages(entry.Images), Source: entry.Source, NeedsReview: entry.NeedsReview})
		}
	}
	return view, nil
}

func (s *Service) ReplaceKR(ctx context.Context, id string, input ReplaceKRInput) (KRView, error) {
	if err := validateReplaceInput(id, input, true); err != nil {
		return KRView{}, err
	}
	owners := normalizeOwners(input.Owners, input.OwnerName, input.OwnerOpenID)
	ownerNames := make([]string, 0, len(owners))
	firstOwnerOpenID := ""
	for _, owner := range owners {
		ownerNames = append(ownerNames, owner.Name)
		if firstOwnerOpenID == "" && owner.OpenID != "" {
			firstOwnerOpenID = owner.OpenID
		}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.KR{}).Where("id = ? AND version = ?", id, input.ExpectedVersion).Updates(map[string]any{
			"title": input.Title, "owner_open_id": firstOwnerOpenID, "owner_name": strings.Join(ownerNames, "、"), "priority": input.Priority,
			"metric_note": input.MetricNote, "updated_by": input.UpdatedBy, "version": gorm.Expr("version + 1"),
		})
		if result.Error != nil {
			return fmt.Errorf("update kr: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&domain.KR{}).Where("id = ?", id).Count(&count).Error; err != nil {
				return fmt.Errorf("check kr conflict: %w", err)
			}
			if count == 0 {
				return ErrNotFound
			}
			return ErrConflict
		}
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KROwner{}).Error; err != nil {
			return fmt.Errorf("replace owners: %w", err)
		}
		for index, owner := range owners {
			if err := tx.Create(&domain.KROwner{KRID: id, PersonID: ownerPersonID(owner), OwnerKey: ownerKey(owner), OpenID: owner.OpenID, Name: owner.Name, SortOrder: index}).Error; err != nil {
				return fmt.Errorf("create owner: %w", err)
			}
		}
		var oldMetricIDs []string
		if err := tx.Model(&domain.KRMetric{}).Where("kr_id = ?", id).Pluck("id", &oldMetricIDs).Error; err != nil {
			return fmt.Errorf("list old metrics: %w", err)
		}
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRMetric{}).Error; err != nil {
			return fmt.Errorf("replace metrics: %w", err)
		}
		newMetricIDs := make(map[string]struct{}, len(input.Metrics))
		for i, item := range input.Metrics {
			newMetricIDs[item.ID] = struct{}{}
			if err := tx.Create(&domain.KRMetric{ID: item.ID, KRID: id, Text: item.Text, Light: item.Light, Images: item.Images, SortOrder: i}).Error; err != nil {
				return fmt.Errorf("create metric: %w", err)
			}
		}
		for _, metricID := range oldMetricIDs {
			if _, kept := newMetricIDs[metricID]; kept {
				continue
			}
			if err := tx.Where("target_id = ?", metricID).Delete(&domain.PageComment{}).Error; err != nil {
				return fmt.Errorf("delete removed metric comments: %w", err)
			}
		}
		var oldPoints []domain.KRPoint
		if err := tx.Where("kr_id = ?", id).Find(&oldPoints).Error; err != nil {
			return fmt.Errorf("list old points: %w", err)
		}
		oldPointIDs := make([]string, 0, len(oldPoints))
		oldPointByID := make(map[string]domain.KRPoint, len(oldPoints))
		for _, oldPoint := range oldPoints {
			oldPointIDs = append(oldPointIDs, oldPoint.ID)
			oldPointByID[oldPoint.ID] = oldPoint
		}
		incomingPointIDs := make(map[string]struct{}, len(input.Points))
		incomingEntryIDs := make(map[string]struct{})
		for _, point := range input.Points {
			incomingPointIDs[point.ID] = struct{}{}
			for _, entry := range point.Entries {
				incomingEntryIDs[entry.ID] = struct{}{}
			}
		}
		for _, oldPoint := range oldPoints {
			if _, kept := incomingPointIDs[oldPoint.ID]; kept {
				continue
			}
			var historicalCount int64
			if err := tx.Model(&domain.KRProgress{}).Where("point_id = ? AND week <> ?", oldPoint.ID, input.Week).Count(&historicalCount).Error; err != nil {
				return fmt.Errorf("check point history: %w", err)
			}
			if historicalCount > 0 {
				return fmt.Errorf("point %s has progress history in another week and cannot be removed", oldPoint.ID)
			}
		}
		var replacedEntryIDs []string
		if len(oldPointIDs) > 0 {
			if err := tx.Model(&domain.KRProgress{}).Where("point_id IN ? AND week = ?", oldPointIDs, input.Week).Pluck("id", &replacedEntryIDs).Error; err != nil {
				return fmt.Errorf("list weekly progress for replacement: %w", err)
			}
			if err := tx.Where("point_id IN ? AND week = ?", oldPointIDs, input.Week).Delete(&domain.KRProgress{}).Error; err != nil {
				return fmt.Errorf("replace weekly progress: %w", err)
			}
		}
		removedEntryIDs := make([]string, 0, len(replacedEntryIDs))
		for _, entryID := range replacedEntryIDs {
			if _, kept := incomingEntryIDs[entryID]; !kept {
				removedEntryIDs = append(removedEntryIDs, entryID)
			}
		}
		if len(removedEntryIDs) > 0 {
			if err := tx.Where("target_id IN ?", removedEntryIDs).Delete(&domain.PageComment{}).Error; err != nil {
				return fmt.Errorf("delete replaced progress comments: %w", err)
			}
		}
		for pointIndex, point := range input.Points {
			linkedID := strings.TrimSpace(point.MeegoWorkItemID)
			if oldPoint, exists := oldPointByID[point.ID]; exists {
				if err := tx.Model(&domain.KRPoint{}).Where("id = ? AND kr_id = ?", point.ID, id).Updates(map[string]any{"kind": point.Kind, "title": point.Title, "meego_work_item_id": linkedID, "meego_url": strings.TrimSpace(point.MeegoURL), "sort_order": pointIndex}).Error; err != nil {
					return fmt.Errorf("update point: %w", err)
				}
				if oldPoint.MeegoWorkItemID != linkedID {
					if err := tx.Where("point_id = ?", point.ID).Delete(&domain.MeegoSyncSnapshot{}).Error; err != nil {
						return fmt.Errorf("reset changed Meego snapshot: %w", err)
					}
				}
			} else if err := tx.Create(&domain.KRPoint{ID: point.ID, KRID: id, Kind: point.Kind, Title: point.Title, MeegoWorkItemID: linkedID, MeegoURL: strings.TrimSpace(point.MeegoURL), SortOrder: pointIndex}).Error; err != nil {
				return fmt.Errorf("create point: %w", err)
			}
			for entryIndex, entry := range point.Entries {
				progress := domain.KRProgress{ID: entry.ID, PointID: point.ID, Week: input.Week, Status: entry.Status, Text: entry.Text, Docs: entry.Docs, Images: entry.Images, Source: normalizedSource(entry.Source), NeedsReview: entry.NeedsReview, SortOrder: entryIndex, CreatedBy: input.UpdatedBy, UpdatedBy: input.UpdatedBy}
				if err := tx.Create(&progress).Error; err != nil {
					return fmt.Errorf("create progress: %w", err)
				}
			}
		}
		for _, oldPoint := range oldPoints {
			if _, kept := incomingPointIDs[oldPoint.ID]; kept {
				continue
			}
			if err := tx.Where("point_id = ?", oldPoint.ID).Delete(&domain.MeegoSyncSnapshot{}).Error; err != nil {
				return fmt.Errorf("delete removed point snapshot: %w", err)
			}
			if err := tx.Where("target_id = ?", oldPoint.ID).Delete(&domain.PageComment{}).Error; err != nil {
				return fmt.Errorf("delete removed point comments: %w", err)
			}
			if err := tx.Where("id = ? AND kr_id = ?", oldPoint.ID, id).Delete(&domain.KRPoint{}).Error; err != nil {
				return fmt.Errorf("delete removed point: %w", err)
			}
		}
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRTag{}).Error; err != nil {
			return fmt.Errorf("replace tags: %w", err)
		}
		for _, tag := range input.Tags {
			if err := tx.Create(&domain.KRTag{KRID: id, Type: tag.Type, Value: tag.Value}).Error; err != nil {
				return fmt.Errorf("create tag: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return KRView{}, err
	}
	return s.GetKR(ctx, id, input.Week)
}

// ReplaceKRCore changes only the stable OKR definition. Weekly progress rows
// are deliberately outside this transaction, so tagging or reassigning an OKR
// can never rewrite a historical/current weekly report as a side effect.
func (s *Service) ReplaceKRCore(ctx context.Context, id string, input ReplaceKRInput) (KRView, error) {
	if err := validateReplaceInput(id, input, false); err != nil {
		return KRView{}, err
	}
	owners := normalizeOwners(input.Owners, input.OwnerName, input.OwnerOpenID)
	ownerNames := make([]string, 0, len(owners))
	firstOwnerOpenID := ""
	for _, owner := range owners {
		ownerNames = append(ownerNames, owner.Name)
		if firstOwnerOpenID == "" && owner.OpenID != "" {
			firstOwnerOpenID = owner.OpenID
		}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := updateKRVersion(tx, id, input.ExpectedVersion, map[string]any{
			"title": input.Title, "owner_open_id": firstOwnerOpenID, "owner_name": strings.Join(ownerNames, "、"),
			"priority": input.Priority, "metric_note": input.MetricNote, "updated_by": input.UpdatedBy,
		}); err != nil {
			return err
		}
		if err := replaceKROwners(tx, id, owners); err != nil {
			return err
		}

		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRMetric{}).Error; err != nil {
			return fmt.Errorf("replace metrics: %w", err)
		}
		for index, metric := range input.Metrics {
			if err := tx.Create(&domain.KRMetric{ID: metric.ID, KRID: id, Text: metric.Text, Light: metric.Light, Images: metric.Images, SortOrder: index}).Error; err != nil {
				return fmt.Errorf("create metric: %w", err)
			}
		}

		var oldPoints []domain.KRPoint
		if err := tx.Where("kr_id = ?", id).Find(&oldPoints).Error; err != nil {
			return fmt.Errorf("list old points: %w", err)
		}
		oldPointByID := make(map[string]domain.KRPoint, len(oldPoints))
		incomingPointIDs := make(map[string]struct{}, len(input.Points))
		for _, point := range oldPoints {
			oldPointByID[point.ID] = point
		}
		for index, point := range input.Points {
			incomingPointIDs[point.ID] = struct{}{}
			linkedID := strings.TrimSpace(point.MeegoWorkItemID)
			if _, exists := oldPointByID[point.ID]; exists {
				if err := tx.Model(&domain.KRPoint{}).Where("id = ? AND kr_id = ?", point.ID, id).Updates(map[string]any{
					"kind": point.Kind, "title": point.Title, "meego_work_item_id": linkedID,
					"meego_url": strings.TrimSpace(point.MeegoURL), "sort_order": index,
				}).Error; err != nil {
					return fmt.Errorf("update point definition: %w", err)
				}
			} else if err := tx.Create(&domain.KRPoint{ID: point.ID, KRID: id, Kind: point.Kind, Title: point.Title, MeegoWorkItemID: linkedID, MeegoURL: strings.TrimSpace(point.MeegoURL), SortOrder: index}).Error; err != nil {
				return fmt.Errorf("create point definition: %w", err)
			}
		}
		for _, point := range oldPoints {
			if _, kept := incomingPointIDs[point.ID]; kept {
				continue
			}
			if err := tx.Where("id = ? AND kr_id = ?", point.ID, id).Delete(&domain.KRPoint{}).Error; err != nil {
				return fmt.Errorf("delete removed point: %w", err)
			}
		}

		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRTag{}).Error; err != nil {
			return fmt.Errorf("replace tags: %w", err)
		}
		for _, tag := range input.Tags {
			if err := tx.Create(&domain.KRTag{KRID: id, Type: tag.Type, Value: tag.Value}).Error; err != nil {
				return fmt.Errorf("create tag: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return KRView{}, err
	}
	return s.GetCoreKR(ctx, id)
}

// ReplaceWeeklyProgress changes only progress entries for the selected week.
// KR titles, metrics, tags, owners and point definitions remain owned by OKR.
func (s *Service) ReplaceWeeklyProgress(ctx context.Context, id string, input ReplaceKRInput) (KRView, error) {
	if err := validateReplaceInput(id, input, true); err != nil {
		return KRView{}, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := updateKRVersion(tx, id, input.ExpectedVersion, map[string]any{"updated_by": input.UpdatedBy}); err != nil {
			return err
		}
		var existingPoints []domain.KRPoint
		if err := tx.Where("kr_id = ?", id).Find(&existingPoints).Error; err != nil {
			return fmt.Errorf("list weekly report points: %w", err)
		}
		existing := make(map[string]struct{}, len(existingPoints))
		for _, point := range existingPoints {
			existing[point.ID] = struct{}{}
		}
		pointIDs := make([]string, 0, len(input.Points))
		incomingEntryIDs := make(map[string]struct{})
		for _, point := range input.Points {
			if _, ok := existing[point.ID]; !ok {
				return fmt.Errorf("weekly report point %s does not belong to kr %s", point.ID, id)
			}
			pointIDs = append(pointIDs, point.ID)
			for _, entry := range point.Entries {
				incomingEntryIDs[entry.ID] = struct{}{}
			}
		}
		var replacedEntryIDs []string
		if len(pointIDs) > 0 {
			if err := tx.Model(&domain.KRProgress{}).Where("point_id IN ? AND week = ?", pointIDs, input.Week).Pluck("id", &replacedEntryIDs).Error; err != nil {
				return fmt.Errorf("list weekly progress for replacement: %w", err)
			}
			if err := tx.Where("point_id IN ? AND week = ?", pointIDs, input.Week).Delete(&domain.KRProgress{}).Error; err != nil {
				return fmt.Errorf("replace weekly progress: %w", err)
			}
		}
		removedEntryIDs := make([]string, 0, len(replacedEntryIDs))
		for _, entryID := range replacedEntryIDs {
			if _, kept := incomingEntryIDs[entryID]; !kept {
				removedEntryIDs = append(removedEntryIDs, entryID)
			}
		}
		if len(removedEntryIDs) > 0 {
			if err := tx.Where("target_id IN ?", removedEntryIDs).Delete(&domain.PageComment{}).Error; err != nil {
				return fmt.Errorf("delete replaced progress comments: %w", err)
			}
		}
		for _, point := range input.Points {
			for index, entry := range point.Entries {
				progress := domain.KRProgress{ID: entry.ID, PointID: point.ID, Week: input.Week, Status: entry.Status, Text: entry.Text, Docs: entry.Docs, Images: entry.Images, Source: normalizedSource(entry.Source), NeedsReview: entry.NeedsReview, SortOrder: index, CreatedBy: input.UpdatedBy, UpdatedBy: input.UpdatedBy}
				if err := tx.Create(&progress).Error; err != nil {
					return fmt.Errorf("create weekly progress: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return KRView{}, err
	}
	return s.GetKR(ctx, id, input.Week)
}

func updateKRVersion(tx *gorm.DB, id string, expectedVersion int32, values map[string]any) error {
	values["version"] = gorm.Expr("version + 1")
	result := tx.Model(&domain.KR{}).Where("id = ? AND version = ?", id, expectedVersion).Updates(values)
	if result.Error != nil {
		return fmt.Errorf("update kr: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var count int64
	if err := tx.Model(&domain.KR{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return fmt.Errorf("check kr conflict: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return ErrConflict
}

func replaceKROwners(tx *gorm.DB, id string, owners []OwnerView) error {
	if err := tx.Where("kr_id = ?", id).Delete(&domain.KROwner{}).Error; err != nil {
		return fmt.Errorf("replace owners: %w", err)
	}
	for index, owner := range owners {
		if err := tx.Create(&domain.KROwner{KRID: id, PersonID: ownerPersonID(owner), OwnerKey: ownerKey(owner), OpenID: owner.OpenID, Name: owner.Name, SortOrder: index}).Error; err != nil {
			return fmt.Errorf("create owner: %w", err)
		}
	}
	return nil
}

func (s *Service) CreateKR(ctx context.Context, objectiveID string, input CreateKRInput) (KRView, error) {
	objectiveID = strings.TrimSpace(objectiveID)
	input.Title = strings.TrimSpace(input.Title)
	input.OwnerName = strings.TrimSpace(input.OwnerName)
	input.Priority = strings.TrimSpace(input.Priority)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if objectiveID == "" || input.Title == "" {
		return KRView{}, fmt.Errorf("objective id and kr title are required")
	}
	if input.Priority != "p0" && input.Priority != "p1" && input.Priority != "p2" {
		return KRView{}, fmt.Errorf("priority must be p0, p1, or p2")
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ?", objectiveID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get objective: %w", err)
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", objectiveID, input.Title, now.UnixNano())))
	id := fmt.Sprintf("kr-%x", digest[:10])
	var maxSort int
	if err := s.db.WithContext(ctx).Model(&domain.KR{}).Where("objective_id = ?", objectiveID).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSort).Error; err != nil {
		return KRView{}, fmt.Errorf("get kr sort order: %w", err)
	}
	record := domain.KR{
		ID: id, ObjectiveID: objectiveID, Title: input.Title, OwnerName: input.OwnerName,
		Priority: input.Priority, SortOrder: maxSort + 1, CreatedBy: input.CreatedBy, UpdatedBy: input.CreatedBy,
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return KRView{}, fmt.Errorf("create kr: %w", err)
	}
	return s.GetCoreKR(ctx, id)
}

func (s *Service) CreateObjective(ctx context.Context, input CreateObjectiveInput) (ObjectiveView, error) {
	input.Quarter = strings.TrimSpace(input.Quarter)
	input.Title = strings.TrimSpace(input.Title)
	if !quarterPattern.MatchString(input.Quarter) {
		return ObjectiveView{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	if input.Title == "" {
		return ObjectiveView{}, fmt.Errorf("objective title is required")
	}
	var maxSort int
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Where("quarter = ?", input.Quarter).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSort).Error; err != nil {
		return ObjectiveView{}, fmt.Errorf("get objective sort order: %w", err)
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", input.Quarter, input.Title, now.UnixNano())))
	record := domain.Objective{ID: fmt.Sprintf("objective-%x", digest[:10]), Quarter: input.Quarter, Title: input.Title, SortOrder: maxSort + 1}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return ObjectiveView{}, fmt.Errorf("create objective: %w", err)
	}
	return ObjectiveView{ID: record.ID, Title: record.Title, KRs: []KRView{}}, nil
}

func (s *Service) DeleteKR(ctx context.Context, id string, input DeleteKRInput) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("kr id is required")
	}
	if input.ExpectedVersion < 0 {
		return fmt.Errorf("expected_version must be non-negative")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record domain.KR
		if err := tx.Where("id = ? AND version = ?", id, input.ExpectedVersion).First(&record).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("get kr for delete: %w", err)
			}
			var count int64
			if err := tx.Model(&domain.KR{}).Where("id = ?", id).Count(&count).Error; err != nil {
				return fmt.Errorf("check kr delete conflict: %w", err)
			}
			if count > 0 {
				return ErrConflict
			}
			return ErrNotFound
		}
		// Weekly-report rows are a separate module's history. Core deletion only
		// removes the stable definition and deliberately leaves that history intact.
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRTag{}).Error; err != nil {
			return fmt.Errorf("delete tags: %w", err)
		}
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRMetric{}).Error; err != nil {
			return fmt.Errorf("delete metrics: %w", err)
		}
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRPoint{}).Error; err != nil {
			return fmt.Errorf("delete points: %w", err)
		}
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KROwner{}).Error; err != nil {
			return fmt.Errorf("delete owner links: %w", err)
		}
		result := tx.Where("id = ? AND version = ?", id, input.ExpectedVersion).Delete(&domain.KR{})
		if result.Error != nil {
			return fmt.Errorf("delete kr: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// MeegoPreview reads the latest observation persisted by the module Skill. HTTP
// requests never invoke bytedcli; external discovery remains prompt/tool owned.
func (s *Service) MeegoPreview(ctx context.Context, pointID, week string) (MeegoPreview, error) {
	if strings.TrimSpace(pointID) == "" || !weekPattern.MatchString(week) {
		return MeegoPreview{}, fmt.Errorf("point_id and YYYY-Www week are required")
	}
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", pointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return MeegoPreview{}, ErrNotFound
		}
		return MeegoPreview{}, fmt.Errorf("get point: %w", err)
	}
	if strings.TrimSpace(point.MeegoWorkItemID) == "" {
		return MeegoPreview{}, fmt.Errorf("point has no Meego work item")
	}
	preview, _, err := s.cachedMeegoPreview(ctx, point, week)
	if err != nil {
		return MeegoPreview{}, err
	}
	if preview == nil {
		return MeegoPreview{}, ErrMeegoUnavailable
	}
	return *preview, nil
}

func (s *Service) localPointPreview(ctx context.Context, pointID, week string) (PreviewContent, error) {
	var entries []domain.KRProgress
	if err := s.db.WithContext(ctx).Where("point_id = ? AND week = ?", pointID, week).Order("sort_order, id").Find(&entries).Error; err != nil {
		return PreviewContent{}, fmt.Errorf("list local progress: %w", err)
	}
	local := PreviewContent{}
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		if local.Status == "" {
			local.Status = string(entry.Status)
		}
		if text := strings.TrimSpace(entry.Text); text != "" {
			texts = append(texts, text)
		}
	}
	local.Progress = strings.Join(texts, "\n")
	return local, nil
}

// StoreMeegoObservation persists one bytedcli result supplied by an Agent. It
// validates the point linkage and derives the diff locally, but deliberately
// contains no external-system query or matching policy.
func (s *Service) StoreMeegoObservation(ctx context.Context, input MeegoObservationInput) (MeegoObservationResult, error) {
	input.PointID = strings.TrimSpace(input.PointID)
	input.WorkItemID = strings.TrimSpace(input.WorkItemID)
	input.FetchError = strings.TrimSpace(input.FetchError)
	if input.PointID == "" || input.WorkItemID == "" || !weekPattern.MatchString(input.Week) || input.ObservedAt.IsZero() {
		return MeegoObservationResult{}, fmt.Errorf("point_id, work_item_id, week and observed_at are required")
	}
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", input.PointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return MeegoObservationResult{}, ErrNotFound
		}
		return MeegoObservationResult{}, fmt.Errorf("get Meego observation point: %w", err)
	}
	if strings.TrimSpace(point.MeegoWorkItemID) != input.WorkItemID {
		return MeegoObservationResult{}, fmt.Errorf("work item does not match the point's current Meego link")
	}
	var snapshot domain.MeegoSyncSnapshot
	err := s.db.WithContext(ctx).First(&snapshot, "point_id = ?", point.ID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return MeegoObservationResult{}, fmt.Errorf("load Meego observation: %w", err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || snapshot.WorkItemID != point.MeegoWorkItemID {
		snapshot = domain.MeegoSyncSnapshot{PointID: point.ID, WorkItemID: point.MeegoWorkItemID}
	}
	snapshot.Week = input.Week
	snapshot.LastAttemptAt = input.ObservedAt.UTC()
	snapshot.LastError = input.FetchError
	if input.FetchError == "" {
		local, err := s.localPointPreview(ctx, point.ID, input.Week)
		if err != nil {
			return MeegoObservationResult{}, err
		}
		remote := input.Remote
		diff := PreviewDiff{
			StatusChanged:   strings.TrimSpace(remote.Status) != "" && normalizeMeegoStatus(remote.Status) != strings.TrimSpace(local.Status),
			ProgressChanged: strings.TrimSpace(remote.Progress) != "" && strings.TrimSpace(remote.Progress) != strings.TrimSpace(local.Progress),
		}
		preview := MeegoPreview{Local: local, Remote: remote, Diff: diff, NeedsReview: diff.StatusChanged || diff.ProgressChanged}
		succeededAt := input.ObservedAt.UTC()
		snapshot.LocalStatus, snapshot.LocalProgress = local.Status, local.Progress
		snapshot.RemoteTitle, snapshot.RemoteStatus = remote.Title, remote.Status
		snapshot.RemoteProgress, snapshot.RemoteUpdatedAt = remote.Progress, remote.UpdatedAt
		snapshot.StatusChanged, snapshot.ProgressChanged = diff.StatusChanged, diff.ProgressChanged
		snapshot.NeedsReview, snapshot.Risk = preview.NeedsReview, previewRisk(preview)
		snapshot.LastSuccessAt = &succeededAt
	}
	if err := s.db.WithContext(ctx).Save(&snapshot).Error; err != nil {
		return MeegoObservationResult{}, fmt.Errorf("save Meego observation: %w", err)
	}
	preview, syncState, err := s.cachedMeegoPreview(ctx, point, input.Week)
	if err != nil {
		return MeegoObservationResult{}, err
	}
	return MeegoObservationResult{Preview: preview, Sync: syncState}, nil
}

// ConfirmMeegoProgress writes only the selected point's Meego-sourced weekly
// progress after an explicit caller confirmation. Manual entries are untouched.
func (s *Service) ConfirmMeegoProgress(ctx context.Context, pointID string, input ConfirmMeegoProgressInput) (KRView, error) {
	pointID = strings.TrimSpace(pointID)
	input.MeegoWorkItemID = strings.TrimSpace(input.MeegoWorkItemID)
	input.Text = strings.TrimSpace(input.Text)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	if pointID == "" || input.ExpectedVersion < 0 || !weekPattern.MatchString(input.Week) || input.MeegoWorkItemID == "" || input.Text == "" || input.UpdatedBy == "" || !domain.ValidStatus(input.Status) {
		return KRView{}, fmt.Errorf("point_id, expected_version, week, updater, linked work item, status, and text are required")
	}

	var krID string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var point domain.KRPoint
		if err := tx.First(&point, "id = ?", pointID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("get Meego confirmation point: %w", err)
		}
		krID = point.KRID
		if strings.TrimSpace(point.MeegoWorkItemID) != input.MeegoWorkItemID {
			return fmt.Errorf("Meego work item link changed; refresh the preview before confirming")
		}
		result := tx.Model(&domain.KR{}).Where("id = ? AND version = ?", point.KRID, input.ExpectedVersion).Updates(map[string]any{
			"updated_by": input.UpdatedBy,
			"version":    gorm.Expr("version + 1"),
		})
		if result.Error != nil {
			return fmt.Errorf("confirm Meego progress version: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&domain.KR{}).Where("id = ?", point.KRID).Count(&count).Error; err != nil {
				return fmt.Errorf("check Meego confirmation conflict: %w", err)
			}
			if count == 0 {
				return ErrNotFound
			}
			return ErrConflict
		}

		docs := []domain.DocLink{}
		if strings.TrimSpace(point.MeegoURL) != "" {
			docs = append(docs, domain.DocLink{ID: "meego-" + input.MeegoWorkItemID, Title: "Meego " + input.MeegoWorkItemID, URL: point.MeegoURL})
		}
		var existing domain.KRProgress
		err := tx.Where("point_id = ? AND week = ? AND source = ?", point.ID, input.Week, "meego").Order("sort_order, id").First(&existing).Error
		if err == nil {
			existing.Status = input.Status
			existing.Text = input.Text
			existing.Docs = docs
			existing.NeedsReview = false
			existing.UpdatedBy = input.UpdatedBy
			if err := tx.Save(&existing).Error; err != nil {
				return fmt.Errorf("update confirmed Meego progress: %w", err)
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find confirmed Meego progress: %w", err)
		}
		var maxSort int
		if err := tx.Model(&domain.KRProgress{}).Where("point_id = ? AND week = ?", point.ID, input.Week).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSort).Error; err != nil {
			return fmt.Errorf("find Meego progress sort order: %w", err)
		}
		digest := sha256.Sum256([]byte(point.ID + "\x00" + input.Week))
		progress := domain.KRProgress{
			ID: fmt.Sprintf("meego-%x", digest[:8]), PointID: point.ID, Week: input.Week,
			Status: input.Status, Text: input.Text, Docs: docs, Images: []domain.ImageRef{}, Source: "meego", NeedsReview: false,
			SortOrder: maxSort + 1, CreatedBy: input.UpdatedBy, UpdatedBy: input.UpdatedBy,
		}
		if err := tx.Create(&progress).Error; err != nil {
			return fmt.Errorf("create confirmed Meego progress: %w", err)
		}
		return nil
	})
	if err != nil {
		return KRView{}, err
	}
	return s.GetKR(ctx, krID, input.Week)
}

func (s *Service) GetKRByPoint(ctx context.Context, pointID, week string) (KRView, error) {
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", strings.TrimSpace(pointID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get kr by point: %w", err)
	}
	return s.GetKR(ctx, point.KRID, week)
}

func normalizeMeegoStatus(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case normalized == "done", normalized == "completed", strings.Contains(normalized, "完成"):
		return string(domain.StatusDone)
	case normalized == "blocked", strings.Contains(normalized, "阻塞"):
		return string(domain.StatusBlocked)
	case normalized == "delayed", strings.Contains(normalized, "延期"):
		return string(domain.StatusDelayed)
	case normalized == "at_risk", normalized == "risk", strings.Contains(normalized, "风险"):
		return string(domain.StatusAtRisk)
	case normalized == "not_started", strings.Contains(normalized, "未开始"):
		return string(domain.StatusNotStarted)
	default:
		return string(domain.StatusInProgress)
	}
}

// MeegoBatchPreview reads only persisted snapshots. Opening the inbox therefore
// never blocks on or invokes the Meego CLI.
func (s *Service) MeegoBatchPreview(ctx context.Context, quarter, week string) (MeegoBatchPreview, error) {
	quarter = strings.TrimSpace(quarter)
	if quarter == "" || !weekPattern.MatchString(week) {
		return MeegoBatchPreview{}, fmt.Errorf("quarter and YYYY-Www week are required")
	}
	result := MeegoBatchPreview{Quarter: quarter, Week: week, Mode: "preview_only", WriteEnabled: false, Items: []MeegoBatchPreviewItem{}}

	var objectives []domain.Objective
	if err := s.db.WithContext(ctx).Where("quarter = ?", quarter).Order("sort_order, id").Find(&objectives).Error; err != nil {
		return MeegoBatchPreview{}, fmt.Errorf("list Meego preview objectives: %w", err)
	}
	for _, objective := range objectives {
		var records []domain.KR
		if err := s.db.WithContext(ctx).Where("objective_id = ?", objective.ID).Order("sort_order, id").Find(&records).Error; err != nil {
			return MeegoBatchPreview{}, fmt.Errorf("list Meego preview krs: %w", err)
		}
		for _, record := range records {
			var points []domain.KRPoint
			if err := s.db.WithContext(ctx).Where("kr_id = ? AND meego_work_item_id <> ''", record.ID).Order("sort_order, id").Find(&points).Error; err != nil {
				return MeegoBatchPreview{}, fmt.Errorf("list linked Meego points: %w", err)
			}
			for _, point := range points {
				item := MeegoBatchPreviewItem{
					ObjectiveID: objective.ID, ObjectiveTitle: objective.Title,
					KRID: record.ID, KRTitle: record.Title, KRVersion: record.Version, OwnerName: record.OwnerName,
					PointID: point.ID, PointTitle: point.Title,
				}
				result.Summary.LinkedCount++
				preview, syncState, err := s.cachedMeegoPreview(ctx, point, week)
				if err != nil {
					return MeegoBatchPreview{}, err
				}
				item.Sync = syncState
				if syncState == nil {
					result.Summary.UnsyncedCount++
				} else {
					if syncState.Status == "error" {
						result.Summary.ErrorCount++
					}
					if syncState.Status == "stale" {
						result.Summary.StaleCount++
					}
				}
				if preview == nil {
					if syncState != nil && syncState.LastError != "" {
						item.Error = syncState.LastError
					}
					result.Items = append(result.Items, item)
					continue
				}
				item.Preview = preview
				item.Risk = previewRisk(*preview)
				result.Summary.ComparedCount++
				if item.Risk {
					result.Summary.RiskCount++
				}
				if preview.NeedsReview {
					result.Summary.NeedsReviewCount++
				}
				result.Items = append(result.Items, item)
			}
		}
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		return meegoBatchRank(result.Items[i]) < meegoBatchRank(result.Items[j])
	})
	return result, nil
}

func (s *Service) cachedMeegoPreview(ctx context.Context, point domain.KRPoint, week string) (*MeegoPreview, *MeegoSyncState, error) {
	var snapshot domain.MeegoSyncSnapshot
	if err := s.db.WithContext(ctx).First(&snapshot, "point_id = ?", point.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("get cached Meego preview for point %s: %w", point.ID, err)
	}
	syncState := meegoSyncStateFromSnapshot(snapshot, week)
	if snapshot.WorkItemID != point.MeegoWorkItemID || snapshot.LastSuccessAt == nil {
		return nil, syncState, nil
	}
	preview := &MeegoPreview{
		PointID: point.ID, WorkItemID: point.MeegoWorkItemID, URL: point.MeegoURL,
		Mode: "preview_only", WriteEnabled: false,
		Local:  PreviewContent{Status: snapshot.LocalStatus, Progress: snapshot.LocalProgress},
		Remote: PreviewContent{Title: snapshot.RemoteTitle, Status: snapshot.RemoteStatus, Progress: snapshot.RemoteProgress, UpdatedAt: snapshot.RemoteUpdatedAt},
		Diff:   PreviewDiff{StatusChanged: snapshot.StatusChanged, ProgressChanged: snapshot.ProgressChanged}, NeedsReview: snapshot.NeedsReview,
	}
	return preview, syncState, nil
}

func (s *Service) meegoSyncState(ctx context.Context, pointID, week string) (*MeegoSyncState, error) {
	var snapshot domain.MeegoSyncSnapshot
	if err := s.db.WithContext(ctx).First(&snapshot, "point_id = ?", pointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get Meego sync state for point %s: %w", pointID, err)
	}
	return meegoSyncStateFromSnapshot(snapshot, week), nil
}

func meegoSyncStateFromSnapshot(snapshot domain.MeegoSyncSnapshot, week string) *MeegoSyncState {
	state := &MeegoSyncState{
		Week: snapshot.Week, Status: "healthy", LastAttemptAt: snapshot.LastAttemptAt.UTC().Format(time.RFC3339), LastError: snapshot.LastError,
	}
	if snapshot.LastSuccessAt != nil {
		state.LastSuccessAt = snapshot.LastSuccessAt.UTC().Format(time.RFC3339)
	}
	if snapshot.LastError != "" {
		state.Status = "error"
	} else if snapshot.Week != week {
		state.Status = "stale"
	}
	return state
}

func previewRisk(preview MeegoPreview) bool {
	for _, status := range []string{preview.Local.Status, preview.Remote.Status} {
		normalized := strings.ToLower(strings.TrimSpace(status))
		if normalized == string(domain.StatusAtRisk) || normalized == string(domain.StatusDelayed) || normalized == string(domain.StatusBlocked) ||
			strings.Contains(normalized, "风险") || strings.Contains(normalized, "延期") || strings.Contains(normalized, "阻塞") {
			return true
		}
	}
	return false
}

func meegoBatchRank(item MeegoBatchPreviewItem) int {
	if item.Risk {
		return 0
	}
	if item.Error != "" || (item.Sync != nil && item.Sync.Status == "error") {
		return 1
	}
	if item.Preview != nil && item.Preview.NeedsReview {
		return 2
	}
	return 3
}

func validateReplaceInput(id string, input ReplaceKRInput, requireWeek bool) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(input.Title) == "" {
		return fmt.Errorf("kr id and title are required")
	}
	if input.ExpectedVersion < 0 {
		return fmt.Errorf("expected_version must be non-negative")
	}
	if requireWeek && !weekPattern.MatchString(input.Week) {
		return fmt.Errorf("week must use YYYY-Www")
	}
	if input.Priority != "p0" && input.Priority != "p1" && input.Priority != "p2" {
		return fmt.Errorf("priority must be p0, p1, or p2")
	}
	seen := map[string]bool{}
	for _, metric := range input.Metrics {
		if strings.TrimSpace(metric.ID) == "" || strings.TrimSpace(metric.Text) == "" || !domain.ValidLight(metric.Light) || seen[metric.ID] {
			return fmt.Errorf("metrics require unique ids, text, and a valid light")
		}
		seen[metric.ID] = true
	}
	for _, point := range input.Points {
		if strings.TrimSpace(point.ID) == "" || strings.TrimSpace(point.Title) == "" || !domain.ValidPointKind(point.Kind) || seen[point.ID] {
			return fmt.Errorf("points require unique ids, title, and a valid kind")
		}
		seen[point.ID] = true
		for _, entry := range point.Entries {
			if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Text) == "" || !domain.ValidStatus(entry.Status) || seen[entry.ID] {
				return fmt.Errorf("progress entries require unique ids, text, and a valid status")
			}
			seen[entry.ID] = true
		}
	}
	seenTags := map[string]bool{}
	for _, tag := range input.Tags {
		key := tag.Type + "\x00" + tag.Value
		if strings.TrimSpace(tag.Type) == "" || strings.TrimSpace(tag.Value) == "" || seenTags[key] {
			return fmt.Errorf("tags require a type and unique value")
		}
		seenTags[key] = true
	}
	return nil
}

func normalizeOwners(input []OwnerView, legacyName, legacyOpenID string) []OwnerView {
	owners := make([]OwnerView, 0, len(input))
	if len(input) > 0 {
		owners = append(owners, input...)
	} else {
		names := strings.FieldsFunc(legacyName, func(r rune) bool {
			return r == '、' || r == ',' || r == '，' || r == ';' || r == '；'
		})
		for index, name := range names {
			owner := OwnerView{Name: strings.TrimSpace(name)}
			if index == 0 {
				owner.OpenID = strings.TrimSpace(legacyOpenID)
			}
			owners = append(owners, owner)
		}
	}

	result := make([]OwnerView, 0, len(owners))
	seen := map[string]struct{}{}
	for _, owner := range owners {
		owner.OpenID = strings.TrimSpace(owner.OpenID)
		owner.Name = strings.TrimSpace(owner.Name)
		if owner.Name == "" {
			continue
		}
		key := strings.ToLower(owner.OpenID)
		if key == "" {
			key = "name:" + strings.ToLower(owner.Name)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, owner)
	}
	return result
}

func ownerKey(owner OwnerView) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(owner.OpenID)) + "\x00" + strings.ToLower(strings.TrimSpace(owner.Name))))
	return fmt.Sprintf("owner-%x", sum[:12])
}

func ownerPersonID(owner OwnerView) uint64 {
	sum := sha256.Sum256([]byte(ownerKey(owner)))
	value := binary.BigEndian.Uint64(sum[:8])
	if value == 0 {
		return 1
	}
	return value
}

func normalizedSource(source string) string {
	if strings.TrimSpace(source) == "" {
		return "manual"
	}
	return source
}
func nonNilDocs(value []domain.DocLink) []domain.DocLink {
	if value == nil {
		return []domain.DocLink{}
	}
	return value
}
func nonNilImages(value []domain.ImageRef) []domain.ImageRef {
	if value == nil {
		return []domain.ImageRef{}
	}
	return value
}

func (s *Service) weeks(ctx context.Context, quarter, selected string) ([]string, string, error) {
	var weeks []string
	err := s.db.WithContext(ctx).Table("okr_workspace_progress AS progress").Distinct("progress.week").
		Joins("JOIN okr_workspace_point AS point ON point.id = progress.point_id").
		Joins("JOIN okr_workspace_kr AS kr ON kr.id = point.kr_id").
		Joins("JOIN okr_workspace_objective AS objective ON objective.id = kr.objective_id").
		Where("objective.quarter = ?", quarter).Order("progress.week DESC").Pluck("progress.week", &weeks).Error
	if err != nil {
		return nil, "", fmt.Errorf("list available weeks: %w", err)
	}
	foundSelected := false
	previous := ""
	for _, week := range weeks {
		if week == selected {
			foundSelected = true
		}
		if previous == "" && week < selected {
			previous = week
		}
	}
	if !foundSelected {
		weeks = append(weeks, selected)
		sort.Sort(sort.Reverse(sort.StringSlice(weeks)))
	}
	return weeks, previous, nil
}

func EnumValues() map[string]any {
	return map[string]any{"statuses": domain.Statuses, "point_kinds": domain.PointKinds, "lights": domain.Lights, "priorities": []string{"p0", "p1", "p2"}}
}
