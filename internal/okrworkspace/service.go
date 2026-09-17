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
	"sync"
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

type Service struct {
	db                  *gorm.DB
	commentNotifier     CommentMentionNotifier
	people              peopleDirectory
	translator          RegionalTranslator
	translationMu       sync.Mutex
	regionalRefreshMu   sync.Mutex
	regionalRefreshJobs map[string]regionalRefreshJob
}

// RegionalTranslator owns model judgment for natural English phrasing. The
// workspace only owns source collection, exact-text caching and board assembly.
type RegionalTranslator interface {
	CacheKey() string
	Translate(ctx context.Context, texts []string) (map[string]string, error)
}

func NewService(db *gorm.DB) (*Service, error) {
	if db == nil {
		return nil, fmt.Errorf("create kr service: db is nil")
	}
	return &Service{db: db, regionalRefreshJobs: make(map[string]regionalRefreshJob)}, nil
}

func (s *Service) SetRegionalTranslator(translator RegionalTranslator) error {
	if s == nil {
		return fmt.Errorf("set regional translator: service is nil")
	}
	if translator == nil {
		return fmt.Errorf("set regional translator: translator is nil")
	}
	s.translator = translator
	return nil
}

// SetCommentMentionNotifier wires the optional external delivery edge without
// coupling comment persistence to one Feishu client implementation. Comments
// without mentions remain usable in tests and installations that do not expose
// the Biz collaboration surface.
func (s *Service) SetCommentMentionNotifier(notifier CommentMentionNotifier) error {
	if s == nil {
		return fmt.Errorf("set comment mention notifier: service is nil")
	}
	if notifier == nil {
		return fmt.Errorf("set comment mention notifier: notifier is nil")
	}
	s.commentNotifier = notifier
	return nil
}

type Board struct {
	Quarter           string                 `json:"quarter"`
	Week              string                 `json:"week"`
	TemplateKey       domain.WeekTemplateKey `json:"template_key"`
	DeleteToken       string                 `json:"delete_token,omitempty"`
	PreviousWeek      string                 `json:"previous_week,omitempty"`
	AvailableQuarters []string               `json:"available_quarters"`
	AvailableWeeks    []string               `json:"available_weeks"`
	Objectives        []ObjectiveView        `json:"objectives"`
}

type Scope struct {
	Quarter string `json:"quarter"`
	Week    string `json:"week"`
}

type CoreScope struct {
	Quarter string `json:"quarter"`
}

// CoreSubjectExists resolves the stable OKR subjects that Jarvis may assess
// through WorldProgress. The OKR module owns existence checks; the world
// model only keeps the open subject type and ID.
func (s *Service) CoreSubjectExists(ctx context.Context, subjectType, subjectID string) (bool, error) {
	subjectType = strings.TrimSpace(subjectType)
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return false, fmt.Errorf("subject_id is required")
	}
	var query *gorm.DB
	switch subjectType {
	case "okr_objective":
		query = s.db.WithContext(ctx).Model(&domain.Objective{}).Where("id = ? AND plan_id = ''", subjectID)
	case "okr_kr":
		query = s.db.WithContext(ctx).Model(&domain.KR{}).
			Joins("JOIN okr_workspace_objective ON okr_workspace_objective.id = okr_workspace_kr.objective_id").
			Where("okr_workspace_kr.id = ? AND okr_workspace_objective.plan_id = ''", subjectID)
	case "okr_point":
		query = s.db.WithContext(ctx).Model(&domain.KRPoint{}).
			Joins("JOIN okr_workspace_kr ON okr_workspace_kr.id = okr_workspace_point.kr_id").
			Joins("JOIN okr_workspace_objective ON okr_workspace_objective.id = okr_workspace_kr.objective_id").
			Where("okr_workspace_point.id = ? AND okr_workspace_objective.plan_id = ''", subjectID)
	default:
		return false, fmt.Errorf("unsupported OKR subject type %q", subjectType)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, fmt.Errorf("find %s subject %s: %w", subjectType, subjectID, err)
	}
	return count > 0, nil
}

func (s *Service) LatestCoreScope(ctx context.Context) (CoreScope, error) {
	quarters, err := s.ListQuarters(ctx)
	if err != nil {
		return CoreScope{}, err
	}
	if len(quarters) == 0 {
		return CoreScope{}, fmt.Errorf("no valid OKR quarter is available")
	}
	return CoreScope{Quarter: quarters[0]}, nil
}

func (s *Service) ListQuarters(ctx context.Context) ([]string, error) {
	var quarters []string
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Distinct().Where("quarter <> '' AND plan_id = ''").Order("quarter DESC").Pluck("quarter", &quarters).Error; err != nil {
		return nil, fmt.Errorf("list OKR quarters: %w", err)
	}
	for _, quarter := range quarters {
		if !quarterPattern.MatchString(quarter) {
			return nil, fmt.Errorf("invalid OKR quarter in storage: %q", quarter)
		}
	}
	return quarters, nil
}

// LatestWeeklyScope returns the newest explicitly opened reporting week. An
// empty week is therefore discoverable before anybody has filled progress.
func (s *Service) LatestWeeklyScope(ctx context.Context) (Scope, error) {
	var scope Scope
	err := s.db.WithContext(ctx).Model(&domain.WeeklyReportWeek{}).
		Select("quarter, week").Order("quarter DESC, week DESC").Limit(1).Scan(&scope).Error
	if err != nil {
		return Scope{}, fmt.Errorf("find latest weekly report scope: %w", err)
	}
	if !quarterPattern.MatchString(scope.Quarter) || !weekPattern.MatchString(scope.Week) {
		return Scope{}, fmt.Errorf("no valid weekly report scope is available")
	}
	return scope, nil
}

func (s *Service) latestWeeklyScopeForQuarter(ctx context.Context, quarter string) (Scope, error) {
	quarter = strings.TrimSpace(quarter)
	if !quarterPattern.MatchString(quarter) {
		return Scope{}, fmt.Errorf("quarter must use YYYY-Qn")
	}
	var scope Scope
	err := s.db.WithContext(ctx).Model(&domain.WeeklyReportWeek{}).
		Select("quarter, week").Where("quarter = ?", quarter).Order("week DESC").Limit(1).Scan(&scope).Error
	if err != nil {
		return Scope{}, fmt.Errorf("find latest weekly report scope for %s: %w", quarter, err)
	}
	if scope.Quarter != quarter || !weekPattern.MatchString(scope.Week) {
		return Scope{}, fmt.Errorf("no valid weekly report scope is available for %s", quarter)
	}
	return scope, nil
}

func (s *Service) resolveWeeklyScope(ctx context.Context, quarter, week string) (Scope, error) {
	quarter = strings.TrimSpace(quarter)
	week = strings.TrimSpace(week)
	if quarter == "" {
		scope, err := s.LatestWeeklyScope(ctx)
		if err != nil {
			return Scope{}, err
		}
		quarter = scope.Quarter
		if week == "" {
			week = scope.Week
		}
	} else if week == "" {
		scope, err := s.latestWeeklyScopeForQuarter(ctx, quarter)
		if err != nil {
			return Scope{}, err
		}
		week = scope.Week
	}
	return Scope{Quarter: quarter, Week: week}, nil
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
	OwnerEmail    string       `json:"owner_email"`
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
	ID      string      `json:"id"`
	Title   string      `json:"title"`
	Version int32       `json:"version"`
	Owners  []OwnerView `json:"owners"`
	KRs     []KRView    `json:"krs"`
}

type KRView struct {
	ID                string       `json:"id"`
	Title             string       `json:"title"`
	DeleteToken       string       `json:"delete_token,omitempty"`
	OwnerEmail        string       `json:"owner_email"`
	OwnerName         string       `json:"owner_name"`
	MetricNote        string       `json:"metric_note"`
	Version           int32        `json:"version"`
	WeeklyCoreVersion int32        `json:"weekly_core_version"`
	Metrics           []MetricView `json:"metrics"`
	Points            []PointView  `json:"points"`
	Tags              []TagView    `json:"tags"`
	Owners            []OwnerView  `json:"owners"`
	Score             *ScoreView   `json:"score,omitempty"`
}

type ScoreView struct {
	Value   float64 `json:"value"`
	Version int32   `json:"version"`
}

type OwnerView = domain.PersonRef

func storedOwnerView(email, name string, unionID ...string) OwnerView {
	view := OwnerView{Email: domain.NormalizeEmail(email), Name: name}
	if len(unionID) > 0 {
		view.UnionID = unionID[0]
	}
	return view
}

type MetricView struct {
	ID     string            `json:"id"`
	Text   string            `json:"text"`
	Light  domain.Light      `json:"light,omitempty"`
	Images []domain.ImageRef `json:"images"`
}

type PointView struct {
	ID              string           `json:"id"`
	Version         int32            `json:"version"`
	Kind            domain.PointKind `json:"kind"`
	Title           string           `json:"title"`
	MeegoWorkItemID string           `json:"meego_work_item_id"`
	MeegoURL        string           `json:"meego_url"`
	Tags            []TagView        `json:"tags"`
	Owners          []OwnerView      `json:"owners"`
	Entries         []ProgressView   `json:"entries"`
	PreviousEntries []ProgressView   `json:"previous_entries"`
	Score           *ScoreView       `json:"score,omitempty"`
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
	ObjectiveID     string          `json:"objective_id"`
	ObjectiveTitle  string          `json:"objective_title"`
	KRID            string          `json:"kr_id"`
	KRTitle         string          `json:"kr_title"`
	ProgressVersion int32           `json:"progress_version"`
	Owners          []OwnerView     `json:"owners"`
	PointID         string          `json:"point_id"`
	PointTitle      string          `json:"point_title"`
	Risk            bool            `json:"risk"`
	Preview         *MeegoPreview   `json:"preview,omitempty"`
	Sync            *MeegoSyncState `json:"sync,omitempty"`
	Error           string          `json:"error,omitempty"`
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
	Version     int32             `json:"version"`
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
	DeleteToken     string       `json:"delete_token,omitempty"`
	UpdatedBy       string       `json:"updated_by"`
	Title           string       `json:"title"`
	MetricNote      string       `json:"metric_note"`
	Metrics         []MetricView `json:"metrics"`
	Points          []PointView  `json:"points"`
	Tags            []TagView    `json:"tags"`
	Owners          []OwnerView  `json:"owners"`
}

// ReplaceGenericKRInput is the reusable OKR aggregate contract. It owns KR
// fields and point structure; existing point content is deliberately ignored.
type ReplaceGenericKRInput struct {
	ExpectedVersion int32              `json:"expected_version"`
	DeleteToken     string             `json:"delete_token,omitempty"`
	UpdatedBy       string             `json:"-"`
	Title           string             `json:"title"`
	MetricNote      string             `json:"metric_note"`
	Metrics         []MetricView       `json:"metrics"`
	Points          []GenericPointView `json:"points"`
	Owners          []OwnerView        `json:"owners"`
}

type GenericPointView struct {
	ID      string           `json:"id"`
	Version int32            `json:"version"`
	Kind    domain.PointKind `json:"kind"`
	Title   string           `json:"title"`
	Owners  []OwnerView      `json:"owners"`
}

type CreateKRInput struct {
	Title     string      `json:"title"`
	Owners    []OwnerView `json:"owners"`
	CreatedBy string      `json:"created_by"`
}

// CreateBizKRInput is the Biz composition contract. Tags are kept out
// of CreateKRInput so the reusable OKR module can be used without migrating
// any Biz-owned tables.
type CreateBizKRInput struct {
	Title     string      `json:"title"`
	Owners    []OwnerView `json:"owners"`
	Tags      []TagView   `json:"tags"`
	CreatedBy string      `json:"created_by"`
}

type CreateObjectiveInput struct {
	Quarter string      `json:"quarter"`
	Title   string      `json:"title"`
	Owners  []OwnerView `json:"owners"`
}

type UpdateObjectiveInput struct {
	ExpectedVersion int32        `json:"expected_version"`
	Title           string       `json:"title"`
	Owners          *[]OwnerView `json:"owners"`
}

type DeleteObjectiveInput struct {
	ExpectedVersion int32 `json:"expected_version"`
}

type DeleteKRInput struct {
	ExpectedVersion int32  `json:"expected_version"`
	DeleteToken     string `json:"delete_token"`
}

func (s *Service) Board(ctx context.Context, quarter, week string) (Board, error) {
	return s.weeklyBoard(ctx, quarter, week, true)
}

// ProgressBoard returns the formal OKR timeline without reading Biz-owned
// labels, scores or external-system projections.
func (s *Service) ProgressBoard(ctx context.Context, quarter, week string) (Board, error) {
	return s.weeklyBoard(ctx, quarter, week, false)
}

func (s *Service) weeklyBoard(ctx context.Context, quarter, week string, includeBiz bool) (Board, error) {
	scope, err := s.resolveWeeklyScope(ctx, quarter, week)
	if err != nil {
		return Board{}, err
	}
	quarter = scope.Quarter
	week = scope.Week
	if !weekPattern.MatchString(week) {
		return Board{}, fmt.Errorf("week must use YYYY-Www")
	}
	openedWeek, err := s.openedWeek(ctx, quarter, week)
	if err != nil {
		return Board{}, err
	}
	var objectives []domain.Objective
	if err := s.db.WithContext(ctx).Where("quarter = ? AND plan_id = ''", quarter).Order("sort_order, id").Find(&objectives).Error; err != nil {
		return Board{}, fmt.Errorf("list objectives: %w", err)
	}
	weeks, previousWeek, err := s.weeks(ctx, quarter, week)
	if err != nil {
		return Board{}, err
	}
	quarters, err := s.ListQuarters(ctx)
	if err != nil {
		return Board{}, err
	}
	result := Board{Quarter: quarter, Week: week, TemplateKey: openedWeek.TemplateKey, PreviousWeek: previousWeek, AvailableQuarters: quarters, AvailableWeeks: weeks, Objectives: make([]ObjectiveView, 0, len(objectives))}
	for _, objective := range objectives {
		var records []domain.KR
		if err := s.db.WithContext(ctx).Where("objective_id = ?", objective.ID).Order("sort_order, id").Find(&records).Error; err != nil {
			return Board{}, fmt.Errorf("list krs: %w", err)
		}
		objectiveOwners, err := s.objectiveOwners(ctx, objective.ID)
		if err != nil {
			return Board{}, err
		}
		view := ObjectiveView{ID: objective.ID, Title: objective.Title, Version: objective.Version, Owners: objectiveOwners, KRs: make([]KRView, 0, len(records))}
		for _, record := range records {
			krView, err := s.loadKR(ctx, record, quarter, week, previousWeek, includeBiz)
			if err != nil {
				return Board{}, err
			}
			view.KRs = append(view.KRs, krView)
		}
		result.Objectives = append(result.Objectives, view)
	}
	deleteToken, err := s.weekDeletionToken(ctx, quarter, week)
	if err != nil {
		return Board{}, err
	}
	result.DeleteToken = deleteToken
	return result, nil
}

// CoreBoard is the stable OKR projection. It intentionally carries no weekly
// entries or history, even though the compatibility DTO is shared with Board.
func (s *Service) CoreBoard(ctx context.Context, quarter string) (Board, error) {
	return s.coreBoard(ctx, quarter, false)
}

// BizCoreBoard is the Biz presentation of stable OKR definitions. It adds
// Biz-owned labels and legacy Meego links without changing the common OKR
// source of truth.
func (s *Service) BizCoreBoard(ctx context.Context, quarter string) (Board, error) {
	return s.coreBoard(ctx, quarter, true)
}

func (s *Service) coreBoard(ctx context.Context, quarter string, includeBiz bool) (Board, error) {
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
	quarters, err := s.ListQuarters(ctx)
	if err != nil {
		return Board{}, err
	}
	var objectives []domain.Objective
	if err := s.db.WithContext(ctx).Where("quarter = ? AND plan_id = ''", quarter).Order("sort_order, id").Find(&objectives).Error; err != nil {
		return Board{}, fmt.Errorf("list objectives: %w", err)
	}
	result := Board{Quarter: quarter, Week: "", TemplateKey: domain.WeekTemplateClassic, AvailableQuarters: quarters, AvailableWeeks: []string{}, Objectives: make([]ObjectiveView, 0, len(objectives))}
	for _, objective := range objectives {
		var records []domain.KR
		if err := s.db.WithContext(ctx).Where("objective_id = ?", objective.ID).Order("sort_order, id").Find(&records).Error; err != nil {
			return Board{}, fmt.Errorf("list krs: %w", err)
		}
		objectiveOwners, err := s.objectiveOwners(ctx, objective.ID)
		if err != nil {
			return Board{}, err
		}
		view := ObjectiveView{ID: objective.ID, Title: objective.Title, Version: objective.Version, Owners: objectiveOwners, KRs: make([]KRView, 0, len(records))}
		for _, record := range records {
			krView, err := s.loadKRDefinition(ctx, record, includeBiz)
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
	scope, err := s.resolveWeeklyScope(ctx, quarter, week)
	if err != nil {
		return ReminderPreview{}, err
	}
	quarter = scope.Quarter
	week = scope.Week
	if !weekPattern.MatchString(week) {
		return ReminderPreview{}, fmt.Errorf("week must use YYYY-Www")
	}
	if err := s.requireOpenWeek(ctx, quarter, week); err != nil {
		return ReminderPreview{}, err
	}

	result := ReminderPreview{
		Quarter: quarter, Week: week, Mode: "preview_only", SendEnabled: false,
		Recipients: []ReminderRecipient{},
	}
	var objectiveIDs []string
	if err := s.db.WithContext(ctx).Model(&domain.Objective{}).Where("quarter = ? AND plan_id = ''", quarter).Pluck("id", &objectiveIDs).Error; err != nil {
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
		email   string
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
		ownersByKR[link.KRID] = append(ownersByKR[link.KRID], storedOwnerView(link.Email, link.Name, link.UnionID))
	}
	owners := map[string]*recipientAccumulator{}
	for _, record := range records {
		recordOwners := ownersByKR[record.ID]
		if len(recordOwners) == 0 {
			recordOwners = []OwnerView{{Name: "未分配"}}
		}
		pointCount := pointCountByKR[record.ID]
		filled := pointCount > 0 && len(filledPointsByKR[record.ID]) == pointCount
		for _, recordOwner := range recordOwners {
			email := strings.TrimSpace(recordOwner.Email)
			name := strings.TrimSpace(recordOwner.Name)
			key := email
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
				owner = &recipientAccumulator{email: email, name: name, missing: []ReminderKR{}}
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
		canRemind := domain.ValidEmail(owner.email)
		recipient := ReminderRecipient{
			OwnerEmail: owner.email, OwnerName: owner.name, DueCount: owner.due,
			FilledCount: owner.filled, MissingCount: missingCount,
			NeedsReminder: missingCount > 0, CanRemind: canRemind,
			MissingKRs: owner.missing,
		}
		if missingCount > 0 {
			titles := make([]string, 0, missingCount)
			for _, item := range owner.missing {
				titles = append(titles, item.Title)
			}
			if canRemind {
				recipient.Message = fmt.Sprintf("%s，你好。本周（%s）KR 进展还有 %d 条待填写：%s。请在周会前打开 Emily 完成更新，谢谢。", owner.name, week, missingCount, strings.Join(titles, "；"))
			} else if owner.name != "未分配" {
				recipient.Message = fmt.Sprintf("负责人%s缺少有效 email，暂时无法催办：%s。请先由人完善负责人身份。", owner.name, strings.Join(titles, "；"))
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
	return s.getWeeklyKR(ctx, id, week, true)
}

// GetProgressKR returns the formal timeline without Biz-owned decorations.
func (s *Service) GetProgressKR(ctx context.Context, id, week string) (KRView, error) {
	return s.getWeeklyKR(ctx, id, week, false)
}

// GetBizKR names the richer product projection explicitly for new callers.
// GetKR remains as a compatibility alias for existing Biz services.
func (s *Service) GetBizKR(ctx context.Context, id, week string) (KRView, error) {
	return s.getWeeklyKR(ctx, id, week, true)
}

func (s *Service) getWeeklyKR(ctx context.Context, id, week string, includeBiz bool) (KRView, error) {
	var record domain.KR
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get kr: %w", err)
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ?", record.ObjectiveID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get objective for kr: %w", err)
	}
	if objective.PlanID != "" {
		return KRView{}, ErrNotFound
	}
	_, previousWeek, err := s.weeks(ctx, objective.Quarter, week)
	if err != nil {
		return KRView{}, err
	}
	return s.loadKR(ctx, record, objective.Quarter, week, previousWeek, includeBiz)
}

func (s *Service) GetCoreKR(ctx context.Context, id string) (KRView, error) {
	var record domain.KR
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get kr: %w", err)
	}
	if record.ObjectiveID != "" {
		var objective domain.Objective
		if err := s.db.WithContext(ctx).First(&objective, "id = ? AND plan_id = ''", record.ObjectiveID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return KRView{}, ErrNotFound
			}
			return KRView{}, fmt.Errorf("get objective for core KR: %w", err)
		}
	}
	return s.loadKRDefinition(ctx, record, false)
}

func (s *Service) GetBizCoreKR(ctx context.Context, id string) (KRView, error) {
	var record domain.KR
	if err := s.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get Biz KR: %w", err)
	}
	if record.ObjectiveID != "" {
		var objective domain.Objective
		if err := s.db.WithContext(ctx).First(&objective, "id = ? AND plan_id = ''", record.ObjectiveID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return KRView{}, ErrNotFound
			}
			return KRView{}, fmt.Errorf("get objective for Biz KR: %w", err)
		}
	}
	return s.loadKRDefinition(ctx, record, true)
}

func (s *Service) loadKRDefinition(ctx context.Context, record domain.KR, includeBiz bool) (KRView, error) {
	return s.loadKRDefinitionWithGuard(ctx, record, includeBiz, true)
}

func (s *Service) loadKRDefinitionWithGuard(ctx context.Context, record domain.KR, includeBiz, includeDeleteGuard bool) (KRView, error) {
	var metrics []domain.KRMetric
	var points []domain.KRPoint
	var tags []domain.KRTag
	var pointTags []domain.PointTag
	var pointOwners []domain.PointOwner
	var owners []domain.KROwner
	if err := s.db.WithContext(ctx).Where("kr_id = ?", record.ID).Order("sort_order, id").Find(&metrics).Error; err != nil {
		return KRView{}, fmt.Errorf("list metrics: %w", err)
	}
	if err := s.db.WithContext(ctx).Where("kr_id = ?", record.ID).Order("sort_order, id").Find(&points).Error; err != nil {
		return KRView{}, fmt.Errorf("list points: %w", err)
	}
	pointIDs := make([]string, 0, len(points))
	for _, point := range points {
		pointIDs = append(pointIDs, point.ID)
	}
	if len(pointIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("point_id IN ?", pointIDs).Order("point_id, sort_order, owner_key, person_id").Find(&pointOwners).Error; err != nil {
			return KRView{}, fmt.Errorf("list point owners: %w", err)
		}
	}
	if includeBiz && len(pointIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("point_id IN ?", pointIDs).Order("point_id, type, value").Find(&pointTags).Error; err != nil {
			return KRView{}, fmt.Errorf("list point tags: %w", err)
		}
	}
	if includeBiz {
		if err := s.db.WithContext(ctx).Where("kr_id = ?", record.ID).Order("type, value").Find(&tags).Error; err != nil {
			return KRView{}, fmt.Errorf("list tags: %w", err)
		}
	}
	if err := s.db.WithContext(ctx).Where("kr_id = ?", record.ID).Order("sort_order, owner_key, person_id").Find(&owners).Error; err != nil {
		return KRView{}, fmt.Errorf("list owners: %w", err)
	}
	view := KRView{ID: record.ID, Title: record.Title, MetricNote: record.MetricNote, Version: record.Version, Metrics: []MetricView{}, Points: []PointView{}, Tags: []TagView{}, Owners: []OwnerView{}}
	for _, owner := range owners {
		view.Owners = append(view.Owners, storedOwnerView(owner.Email, owner.Name, owner.UnionID))
		if view.OwnerEmail == "" && strings.TrimSpace(owner.Email) != "" {
			view.OwnerEmail = owner.Email
		}
	}
	ownerNames := make([]string, 0, len(view.Owners))
	for _, owner := range view.Owners {
		ownerNames = append(ownerNames, owner.Name)
	}
	view.OwnerName = strings.Join(ownerNames, "、")
	for _, metric := range metrics {
		light := metric.Light
		if light == "" {
			light = domain.LightGreen
		}
		view.Metrics = append(view.Metrics, MetricView{ID: metric.ID, Text: metric.Text, Light: light, Images: nonNilImages(metric.Images)})
	}
	pointTagsByID := make(map[string][]TagView, len(points))
	for _, tag := range pointTags {
		pointTagsByID[tag.PointID] = append(pointTagsByID[tag.PointID], TagView{Type: tag.Type, Value: tag.Value})
	}
	pointOwnersByID := make(map[string][]OwnerView, len(points))
	for _, owner := range pointOwners {
		pointOwnersByID[owner.PointID] = append(pointOwnersByID[owner.PointID], storedOwnerView(owner.Email, owner.Name, owner.UnionID))
	}
	for _, point := range points {
		pointView := PointView{ID: point.ID, Version: point.Version, Kind: point.Kind, Title: point.Title, Tags: pointTagsByID[point.ID], Owners: pointOwnersByID[point.ID], Entries: []ProgressView{}, PreviousEntries: []ProgressView{}}
		if includeBiz {
			pointView.MeegoWorkItemID = point.MeegoWorkItemID
			pointView.MeegoURL = point.MeegoURL
		}
		if pointView.Tags == nil {
			pointView.Tags = []TagView{}
		}
		if pointView.Owners == nil {
			pointView.Owners = []OwnerView{}
		}
		if err := validatePointTags(pointView.Tags); err != nil {
			return KRView{}, fmt.Errorf("invalid tags for point %s: %w", point.ID, err)
		}
		view.Points = append(view.Points, pointView)
	}
	for _, tag := range tags {
		view.Tags = append(view.Tags, TagView{Type: tag.Type, Value: tag.Value})
	}
	if err := validateTags(view.Tags); err != nil {
		return KRView{}, fmt.Errorf("invalid tags for KR %s: %w", record.ID, err)
	}
	if includeDeleteGuard && record.ObjectiveID != "" {
		var objective domain.Objective
		if err := s.db.WithContext(ctx).First(&objective, "id = ?", record.ObjectiveID).Error; err != nil {
			return KRView{}, fmt.Errorf("get KR objective for deletion guard: %w", err)
		}
		if objective.PlanID == "" {
			deleteToken, err := s.krDeletionToken(ctx, record.ID)
			if err != nil {
				return KRView{}, fmt.Errorf("build KR deletion guard: %w", err)
			}
			view.DeleteToken = deleteToken
		}
	}
	return view, nil
}

func (s *Service) loadKR(ctx context.Context, record domain.KR, quarter, week, previousWeek string, includeBiz bool) (KRView, error) {
	view, err := s.loadKRDefinitionWithGuard(ctx, record, includeBiz, false)
	if err != nil {
		return KRView{}, err
	}
	var weeklyCore domain.WeeklyKRCore
	if err := s.db.WithContext(ctx).First(&weeklyCore, "kr_id = ? AND week = ?", record.ID, week).Error; err == nil {
		view.MetricNote = weeklyCore.MetricNote
		view.WeeklyCoreVersion = weeklyCore.Version
		view.Metrics = make([]MetricView, 0, len(weeklyCore.Metrics))
		for _, metric := range weeklyCore.Metrics {
			view.Metrics = append(view.Metrics, MetricView{ID: metric.ID, Text: metric.Text, Light: metric.Light, Images: nonNilImages(metric.Images)})
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return KRView{}, fmt.Errorf("load weekly core data: %w", err)
	}
	pointScores := make(map[string]*ScoreView)
	if includeBiz {
		targetIDs := make([]string, 0, len(view.Points)+1)
		targetIDs = append(targetIDs, record.ID)
		for _, point := range view.Points {
			targetIDs = append(targetIDs, point.ID)
		}
		var scores []domain.WeeklyScore
		if err := s.db.WithContext(ctx).
			Where("quarter = ? AND week = ? AND target_id IN ?", quarter, week, targetIDs).
			Find(&scores).Error; err != nil {
			return KRView{}, fmt.Errorf("list weekly scores: %w", err)
		}
		for _, score := range scores {
			if err := validateStoredWeeklyScore(score); err != nil {
				return KRView{}, err
			}
			scoreView := &ScoreView{Value: score.Score, Version: score.Version}
			switch score.TargetKind {
			case domain.WeeklyScoreTargetKR:
				if score.TargetID != record.ID {
					return KRView{}, fmt.Errorf("weekly score target %s is not KR %s", score.TargetID, record.ID)
				}
				view.Score = scoreView
			case domain.WeeklyScoreTargetPoint:
				pointScores[score.TargetID] = scoreView
			}
		}
	}
	for index := range view.Points {
		point := &view.Points[index]
		point.Score = pointScores[point.ID]
		var progress []domain.KRProgress
		if err := s.db.WithContext(ctx).Where("point_id = ? AND week = ?", point.ID, week).Order("sort_order, id").Find(&progress).Error; err != nil {
			return KRView{}, fmt.Errorf("list progress: %w", err)
		}
		for _, entry := range progress {
			point.Entries = append(point.Entries, ProgressView{ID: entry.ID, Version: entry.Version, Status: entry.Status, Text: entry.Text, Docs: nonNilDocs(entry.Docs), Images: nonNilImages(entry.Images), Source: entry.Source, NeedsReview: entry.NeedsReview})
		}
		if previousWeek == "" {
			continue
		}
		var previous []domain.KRProgress
		if err := s.db.WithContext(ctx).Where("point_id = ? AND week = ?", point.ID, previousWeek).Order("sort_order, id").Find(&previous).Error; err != nil {
			return KRView{}, fmt.Errorf("list previous progress: %w", err)
		}
		for _, entry := range previous {
			point.PreviousEntries = append(point.PreviousEntries, ProgressView{ID: entry.ID, Version: entry.Version, Status: entry.Status, Text: entry.Text, Docs: nonNilDocs(entry.Docs), Images: nonNilImages(entry.Images), Source: entry.Source, NeedsReview: entry.NeedsReview})
		}
	}
	return view, nil
}

// ReplaceKRCore changes only the stable OKR definition. Weekly progress rows
// are deliberately outside this transaction, so tagging or reassigning an OKR
// can never rewrite a historical/current weekly report as a side effect.
func (s *Service) ReplaceKRCore(ctx context.Context, id string, input ReplaceKRInput) (KRView, error) {
	input.Tags = normalizeTags(input.Tags)
	for index := range input.Points {
		if input.Points[index].Tags != nil {
			input.Points[index].Tags = normalizeTags(input.Points[index].Tags)
		}
	}
	if err := validateReplaceInput(id, input, true); err != nil {
		return KRView{}, err
	}
	return s.replaceKRCore(ctx, id, input, true)
}

func (s *Service) ReplaceGenericKRCore(ctx context.Context, id string, input ReplaceGenericKRInput) (KRView, error) {
	common := ReplaceKRInput{
		ExpectedVersion: input.ExpectedVersion, DeleteToken: input.DeleteToken, UpdatedBy: input.UpdatedBy,
		Title: input.Title, MetricNote: input.MetricNote, Metrics: input.Metrics, Owners: input.Owners,
		Points: make([]PointView, 0, len(input.Points)),
	}
	for _, point := range input.Points {
		common.Points = append(common.Points, PointView{ID: point.ID, Version: point.Version, Kind: point.Kind, Title: point.Title, Owners: point.Owners})
	}
	if err := validateReplaceInput(id, common, false); err != nil {
		return KRView{}, err
	}
	return s.replaceKRCore(ctx, id, common, false)
}

func (s *Service) replaceKRCore(ctx context.Context, id string, input ReplaceKRInput, includeBiz bool) (KRView, error) {
	owners := normalizeOwners(input.Owners)
	if err := s.verifyPeople(ctx, owners); err != nil {
		return KRView{}, err
	}
	for i := range input.Points {
		if err := s.verifyPeople(ctx, input.Points[i].Owners); err != nil {
			return KRView{}, err
		}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		weeklySchemaPresent := tx.Migrator().HasTable(&domain.KRProgress{})
		removesChildren, err := krDefinitionRemovesChildren(tx, id, input)
		if err != nil {
			return err
		}
		if removesChildren {
			guardService := Service{db: tx}
			currentToken, err := guardService.krDeletionToken(ctx, id)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrNotFound
				}
				return fmt.Errorf("read KR structure guard: %w", err)
			}
			if strings.TrimSpace(input.DeleteToken) == "" || currentToken != strings.TrimSpace(input.DeleteToken) {
				return ErrConflict
			}
		}
		if err := updateKRVersion(tx, id, input.ExpectedVersion, map[string]any{
			"title": input.Title, "metric_note": input.MetricNote, "updated_by": input.UpdatedBy,
		}); err != nil {
			return err
		}
		if err := replaceKROwners(tx, id, owners); err != nil {
			return err
		}

		var oldMetricIDs []string
		if err := tx.Model(&domain.KRMetric{}).Where("kr_id = ?", id).Pluck("id", &oldMetricIDs).Error; err != nil {
			return fmt.Errorf("list old metrics: %w", err)
		}
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRMetric{}).Error; err != nil {
			return fmt.Errorf("replace metrics: %w", err)
		}
		incomingMetricIDs := make(map[string]struct{}, len(input.Metrics))
		for index, metric := range input.Metrics {
			incomingMetricIDs[metric.ID] = struct{}{}
			if err := tx.Create(&domain.KRMetric{ID: metric.ID, KRID: id, Text: metric.Text, Light: metric.Light, Images: metric.Images, SortOrder: index}).Error; err != nil {
				return fmt.Errorf("create metric: %w", err)
			}
		}
		for _, metricID := range oldMetricIDs {
			if _, kept := incomingMetricIDs[metricID]; kept {
				continue
			}
			if weeklySchemaPresent && tx.Migrator().HasTable(&domain.PageComment{}) {
				if err := tx.Where("target_id = ?", metricID).Delete(&domain.PageComment{}).Error; err != nil {
					return fmt.Errorf("delete removed metric comments: %w", err)
				}
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
			_, exists := oldPointByID[point.ID]
			if exists {
				// Existing point content is owned exclusively by the point PATCH
				// endpoint. A KR snapshot may only preserve structural ordering.
				if err := tx.Model(&domain.KRPoint{}).Where("id = ? AND kr_id = ?", point.ID, id).Update("sort_order", index).Error; err != nil {
					return fmt.Errorf("update point definition: %w", err)
				}
			} else {
				if strings.TrimSpace(point.Title) == "" {
					return fmt.Errorf("new points require a title")
				}
				if !domain.ValidPointKind(point.Kind) {
					return fmt.Errorf("new points require a valid kind")
				}
				if includeBiz && point.Tags == nil {
					return fmt.Errorf("new points require tags; use [] for no tags")
				}
				record := domain.KRPoint{ID: point.ID, KRID: id, Kind: point.Kind, Title: point.Title, SortOrder: index}
				if includeBiz {
					record.MeegoWorkItemID = strings.TrimSpace(point.MeegoWorkItemID)
					record.MeegoURL = strings.TrimSpace(point.MeegoURL)
				}
				if err := tx.Create(&record).Error; err != nil {
					return fmt.Errorf("create point definition: %w", err)
				}
			}
			if includeBiz && !exists {
				if err := tx.Where("point_id = ?", point.ID).Delete(&domain.PointTag{}).Error; err != nil {
					return fmt.Errorf("replace point tags: %w", err)
				}
				for _, tag := range point.Tags {
					if err := tx.Create(&domain.PointTag{PointID: point.ID, Type: tag.Type, Value: tag.Value}).Error; err != nil {
						return fmt.Errorf("create point tag: %w", err)
					}
				}
			}
			if !exists {
				if err := replacePointOwners(tx, point.ID, normalizeOwners(point.Owners)); err != nil {
					return err
				}
			}
		}
		var removedPointIDs []string
		for _, point := range oldPoints {
			if _, kept := incomingPointIDs[point.ID]; !kept {
				removedPointIDs = append(removedPointIDs, point.ID)
			}
		}
		if err := purgePoints(tx, removedPointIDs, weeklySchemaPresent); err != nil {
			return err
		}

		if includeBiz {
			if err := tx.Where("kr_id = ?", id).Delete(&domain.KRTag{}).Error; err != nil {
				return fmt.Errorf("replace tags: %w", err)
			}
			for _, tag := range input.Tags {
				if err := tx.Create(&domain.KRTag{KRID: id, Type: tag.Type, Value: tag.Value}).Error; err != nil {
					return fmt.Errorf("create tag: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return KRView{}, err
	}
	if includeBiz {
		return s.GetBizCoreKR(ctx, id)
	}
	return s.GetCoreKR(ctx, id)
}

func updateKRVersion(tx *gorm.DB, id string, expectedVersion int32, values map[string]any) error {
	var record domain.KR
	if err := tx.Select("objective_id").First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("get kr scope: %w", err)
	}
	if record.ObjectiveID != "" {
		var count int64
		if err := tx.Model(&domain.Objective{}).Where("id = ? AND plan_id = ''", record.ObjectiveID).Count(&count).Error; err != nil {
			return fmt.Errorf("check kr scope: %w", err)
		}
		if count == 0 {
			return ErrNotFound
		}
	}
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
		if owner.Email != "" && !domain.ValidEmail(owner.Email) {
			return fmt.Errorf("负责人邮箱无效，请刷新后重新选人")
		}
		if err := tx.Create(&domain.KROwner{KRID: id, PersonID: ownerPersonID(owner), OwnerKey: ownerKey(owner), Email: domain.NormalizeEmail(owner.Email), UnionID: owner.UnionID, Name: owner.Name, SortOrder: index}).Error; err != nil {
			return fmt.Errorf("create owner: %w", err)
		}
	}
	return nil
}

func replaceObjectiveOwners(tx *gorm.DB, objectiveID string, owners []OwnerView) error {
	if err := tx.Where("objective_id = ?", objectiveID).Delete(&domain.ObjectiveOwner{}).Error; err != nil {
		return fmt.Errorf("replace objective owners: %w", err)
	}
	for index, owner := range owners {
		if owner.Email != "" && !domain.ValidEmail(owner.Email) {
			return fmt.Errorf("负责人邮箱无效，请刷新后重新选人")
		}
		if err := tx.Create(&domain.ObjectiveOwner{ObjectiveID: objectiveID, PersonID: ownerPersonID(owner), OwnerKey: ownerKey(owner), Email: domain.NormalizeEmail(owner.Email), UnionID: owner.UnionID, Name: owner.Name, SortOrder: index}).Error; err != nil {
			return fmt.Errorf("create objective owner: %w", err)
		}
	}
	return nil
}

func (s *Service) objectiveOwners(ctx context.Context, objectiveID string) ([]OwnerView, error) {
	var rows []domain.ObjectiveOwner
	if err := s.db.WithContext(ctx).Where("objective_id = ?", objectiveID).Order("sort_order, owner_key, person_id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list objective owners: %w", err)
	}
	owners := make([]OwnerView, 0, len(rows))
	for _, row := range rows {
		owners = append(owners, storedOwnerView(row.Email, row.Name, row.UnionID))
	}
	return owners, nil
}

func replacePointOwners(tx *gorm.DB, pointID string, owners []OwnerView) error {
	if err := tx.Where("point_id = ?", pointID).Delete(&domain.PointOwner{}).Error; err != nil {
		return fmt.Errorf("replace point owners: %w", err)
	}
	for index, owner := range owners {
		if owner.Email != "" && !domain.ValidEmail(owner.Email) {
			return fmt.Errorf("负责人邮箱无效，请刷新后重新选人")
		}
		if err := tx.Create(&domain.PointOwner{PointID: pointID, PersonID: ownerPersonID(owner), OwnerKey: ownerKey(owner), Email: domain.NormalizeEmail(owner.Email), UnionID: owner.UnionID, Name: owner.Name, SortOrder: index}).Error; err != nil {
			return fmt.Errorf("create point owner: %w", err)
		}
	}
	return nil
}

// purgePoints removes points and everything keyed to them, weekly progress and
// scores included. Every week renders from these same point rows, so a removed
// point has no week left to appear on and its progress would be unreachable.
func purgePoints(tx *gorm.DB, pointIDs []string, weeklySchemaPresent bool) error {
	if len(pointIDs) == 0 {
		return nil
	}
	if weeklySchemaPresent {
		if err := tx.Where("point_id IN ?", pointIDs).Delete(&domain.KRProgress{}).Error; err != nil {
			return fmt.Errorf("delete point progress: %w", err)
		}
		if tx.Migrator().HasTable(&domain.WeeklyScore{}) {
			if err := tx.Where("target_kind = ? AND target_id IN ?", domain.WeeklyScoreTargetPoint, pointIDs).Delete(&domain.WeeklyScore{}).Error; err != nil {
				return fmt.Errorf("delete point weekly scores: %w", err)
			}
		}
		if tx.Migrator().HasTable(&domain.MeegoSyncSnapshot{}) {
			if err := tx.Where("point_id IN ?", pointIDs).Delete(&domain.MeegoSyncSnapshot{}).Error; err != nil {
				return fmt.Errorf("delete point snapshots: %w", err)
			}
		}
		if tx.Migrator().HasTable(&domain.PageComment{}) {
			if err := tx.Where("target_id IN ?", pointIDs).Delete(&domain.PageComment{}).Error; err != nil {
				return fmt.Errorf("delete point comments: %w", err)
			}
		}
	}
	if tx.Migrator().HasTable(&domain.PointTag{}) {
		if err := tx.Where("point_id IN ?", pointIDs).Delete(&domain.PointTag{}).Error; err != nil {
			return fmt.Errorf("delete point tags: %w", err)
		}
	}
	if err := tx.Where("point_id IN ?", pointIDs).Delete(&domain.PointOwner{}).Error; err != nil {
		return fmt.Errorf("delete point owners: %w", err)
	}
	if err := tx.Where("id IN ?", pointIDs).Delete(&domain.KRPoint{}).Error; err != nil {
		return fmt.Errorf("delete points: %w", err)
	}
	return nil
}

func (s *Service) CreateKR(ctx context.Context, objectiveID string, input CreateKRInput) (KRView, error) {
	id, err := s.createKR(ctx, objectiveID, input.Title, input.Owners, input.CreatedBy)
	if err != nil {
		return KRView{}, err
	}
	return s.GetCoreKR(ctx, id)
}

func (s *Service) CreateBizKR(ctx context.Context, objectiveID string, input CreateBizKRInput) (KRView, error) {
	input.Tags = normalizeTags(input.Tags)
	if err := validateTags(input.Tags); err != nil {
		return KRView{}, err
	}
	id, err := s.createKR(ctx, objectiveID, input.Title, input.Owners, input.CreatedBy)
	if err != nil {
		return KRView{}, err
	}
	for _, tag := range input.Tags {
		if err := s.db.WithContext(ctx).Create(&domain.KRTag{KRID: id, Type: tag.Type, Value: tag.Value}).Error; err != nil {
			return KRView{}, fmt.Errorf("create KR tag: %w", err)
		}
	}
	return s.GetBizCoreKR(ctx, id)
}

func (s *Service) createKR(ctx context.Context, objectiveID, title string, owners []OwnerView, createdBy string) (string, error) {
	if err := s.verifyPeople(ctx, owners); err != nil {
		return "", err
	}
	objectiveID = strings.TrimSpace(objectiveID)
	title = strings.TrimSpace(title)
	createdBy = strings.TrimSpace(createdBy)
	if objectiveID == "" || title == "" {
		return "", fmt.Errorf("objective id and kr title are required")
	}
	var objective domain.Objective
	if err := s.db.WithContext(ctx).First(&objective, "id = ? AND plan_id = ''", objectiveID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get objective: %w", err)
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", objectiveID, title, now.UnixNano())))
	id := fmt.Sprintf("kr-%x", digest[:10])
	var maxSort int
	if err := s.db.WithContext(ctx).Model(&domain.KR{}).Where("objective_id = ?", objectiveID).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSort).Error; err != nil {
		return "", fmt.Errorf("get kr sort order: %w", err)
	}
	record := domain.KR{
		ID: id, ObjectiveID: objectiveID, Title: title,
		SortOrder: maxSort + 1, CreatedBy: createdBy, UpdatedBy: createdBy,
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return "", fmt.Errorf("create kr: %w", err)
	}
	if err := replaceKROwners(s.db.WithContext(ctx), id, normalizeOwners(owners)); err != nil {
		return "", err
	}
	return id, nil
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
	if err := s.verifyPeople(ctx, input.Owners); err != nil {
		return ObjectiveView{}, err
	}
	input.Owners = normalizeOwners(input.Owners)
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", input.Quarter, input.Title, now.UnixNano())))
	record := domain.Objective{ID: fmt.Sprintf("objective-%x", digest[:10]), Quarter: input.Quarter, Title: input.Title}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var maxSort int
		if err := tx.Model(&domain.Objective{}).Where("quarter = ? AND plan_id = ''", input.Quarter).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSort).Error; err != nil {
			return fmt.Errorf("get objective sort order: %w", err)
		}
		record.SortOrder = maxSort + 1
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("create objective: %w", err)
		}
		return replaceObjectiveOwners(tx, record.ID, input.Owners)
	}); err != nil {
		return ObjectiveView{}, err
	}
	return ObjectiveView{ID: record.ID, Title: record.Title, Version: record.Version, Owners: input.Owners, KRs: []KRView{}}, nil
}

func (s *Service) UpdateObjective(ctx context.Context, id string, input UpdateObjectiveInput) (ObjectiveView, error) {
	id = strings.TrimSpace(id)
	input.Title = strings.TrimSpace(input.Title)
	if id == "" || input.Title == "" {
		return ObjectiveView{}, fmt.Errorf("objective id and title are required")
	}
	if input.Owners != nil {
		if err := s.verifyPeople(ctx, *input.Owners); err != nil {
			return ObjectiveView{}, err
		}
		normalized := normalizeOwners(*input.Owners)
		input.Owners = &normalized
	}
	var rowsAffected int64
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&domain.Objective{}).
			Where("id = ? AND plan_id = '' AND version = ?", id, input.ExpectedVersion).
			Updates(map[string]any{"title": input.Title, "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return fmt.Errorf("update objective: %w", result.Error)
		}
		rowsAffected = result.RowsAffected
		if rowsAffected != 1 {
			return nil
		}
		if input.Owners != nil {
			return replaceObjectiveOwners(tx, id, *input.Owners)
		}
		return nil
	}); err != nil {
		return ObjectiveView{}, err
	}
	var record domain.Objective
	if err := s.db.WithContext(ctx).First(&record, "id = ? AND plan_id = ''", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ObjectiveView{}, ErrNotFound
		}
		return ObjectiveView{}, fmt.Errorf("read updated objective: %w", err)
	}
	owners, err := s.objectiveOwners(ctx, record.ID)
	if err != nil {
		return ObjectiveView{}, err
	}
	view := ObjectiveView{ID: record.ID, Title: record.Title, Version: record.Version, Owners: owners, KRs: []KRView{}}
	if rowsAffected != 1 {
		return view, ErrConflict
	}
	return view, nil
}

func (s *Service) DeleteObjective(ctx context.Context, id string, input DeleteObjectiveInput) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("objective id is required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record domain.Objective
		if err := tx.First(&record, "id = ? AND plan_id = ''", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("get objective for delete: %w", err)
		}
		var krCount int64
		if err := tx.Model(&domain.KR{}).Where("objective_id = ?", id).Count(&krCount).Error; err != nil {
			return fmt.Errorf("count objective KRs: %w", err)
		}
		if krCount > 0 {
			return fmt.Errorf("objective %s still has %d KRs and cannot be deleted", id, krCount)
		}
		result := tx.Where("id = ? AND version = ?", id, input.ExpectedVersion).Delete(&domain.Objective{})
		if result.Error != nil {
			return fmt.Errorf("delete objective: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.Where("objective_id = ?", id).Delete(&domain.ObjectiveOwner{}).Error; err != nil {
			return fmt.Errorf("delete objective owners: %w", err)
		}
		return nil
	})
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
		guardService := Service{db: tx}
		currentToken, err := guardService.krDeletionToken(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("read KR deletion guard: %w", err)
		}
		if strings.TrimSpace(input.DeleteToken) == "" || currentToken != strings.TrimSpace(input.DeleteToken) {
			return ErrConflict
		}
		weeklySchemaPresent := tx.Migrator().HasTable(&domain.KRProgress{})
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
		if record.ObjectiveID != "" {
			var count int64
			if err := tx.Model(&domain.Objective{}).Where("id = ? AND plan_id = ''", record.ObjectiveID).Count(&count).Error; err != nil {
				return fmt.Errorf("check KR scope for delete: %w", err)
			}
			if count == 0 {
				return ErrNotFound
			}
		}
		var points []domain.KRPoint
		if err := tx.Where("kr_id = ?", id).Find(&points).Error; err != nil {
			return fmt.Errorf("list KR points for delete: %w", err)
		}
		pointIDs := make([]string, 0, len(points))
		for _, point := range points {
			pointIDs = append(pointIDs, point.ID)
		}
		if err := purgePoints(tx, pointIDs, weeklySchemaPresent); err != nil {
			return err
		}
		if weeklySchemaPresent {
			if err := tx.Where("kr_id = ?", id).Delete(&domain.WeeklyKRCore{}).Error; err != nil {
				return fmt.Errorf("delete KR weekly core: %w", err)
			}
			if tx.Migrator().HasTable(&domain.WeeklyScore{}) {
				if err := tx.Where("target_kind = ? AND target_id = ?", domain.WeeklyScoreTargetKR, id).Delete(&domain.WeeklyScore{}).Error; err != nil {
					return fmt.Errorf("delete KR weekly scores: %w", err)
				}
			}
		}
		var metricIDs []string
		if err := tx.Model(&domain.KRMetric{}).Where("kr_id = ?", id).Pluck("id", &metricIDs).Error; err != nil {
			return fmt.Errorf("list KR metrics for delete: %w", err)
		}
		if weeklySchemaPresent && tx.Migrator().HasTable(&domain.PageComment{}) {
			commentTargetIDs := append([]string{id}, metricIDs...)
			if err := tx.Where("target_id IN ?", commentTargetIDs).Delete(&domain.PageComment{}).Error; err != nil {
				return fmt.Errorf("delete KR comments: %w", err)
			}
		}
		if tx.Migrator().HasTable(&domain.KRTag{}) {
			if err := tx.Where("kr_id = ?", id).Delete(&domain.KRTag{}).Error; err != nil {
				return fmt.Errorf("delete tags: %w", err)
			}
		}
		if err := tx.Where("kr_id = ?", id).Delete(&domain.KRMetric{}).Error; err != nil {
			return fmt.Errorf("delete metrics: %w", err)
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
	_, objective, err := s.krObjective(ctx, point.KRID)
	if err != nil {
		return MeegoPreview{}, err
	}
	if err := s.requireOpenWeek(ctx, objective.Quarter, week); err != nil {
		return MeegoPreview{}, err
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
	_, objective, err := s.krObjective(ctx, point.KRID)
	if err != nil {
		return MeegoObservationResult{}, err
	}
	if err := s.requireOpenWeek(ctx, objective.Quarter, input.Week); err != nil {
		return MeegoObservationResult{}, err
	}
	var snapshot domain.MeegoSyncSnapshot
	err = s.db.WithContext(ctx).First(&snapshot, "point_id = ?", point.ID).Error
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
	_, _, objective, err := s.progressPointOwner(ctx, pointID)
	if err != nil {
		return KRView{}, err
	}
	if err := s.requireOpenWeek(ctx, objective.Quarter, input.Week); err != nil {
		return KRView{}, err
	}

	var krID string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		docs := []domain.DocLink{}
		if strings.TrimSpace(point.MeegoURL) != "" {
			docs = append(docs, domain.DocLink{ID: "meego-" + input.MeegoWorkItemID, Title: "Meego " + input.MeegoWorkItemID, URL: point.MeegoURL})
		}
		docsJSON, err := encodeJSONColumn("confirmed Meego progress docs", docs)
		if err != nil {
			return err
		}
		var existing domain.KRProgress
		err = tx.Where("point_id = ? AND week = ? AND source = ?", point.ID, input.Week, "meego").Order("sort_order, id").First(&existing).Error
		if err == nil {
			if input.ExpectedVersion == 0 {
				return ErrConflict
			}
			result := tx.Model(&domain.KRProgress{}).Where("id = ? AND version = ?", existing.ID, input.ExpectedVersion).Updates(map[string]any{
				"status": input.Status, "text": input.Text, "docs": docsJSON, "needs_review": false,
				"updated_by": input.UpdatedBy, "version": gorm.Expr("version + 1"),
			})
			if result.Error != nil {
				return fmt.Errorf("update confirmed Meego progress: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find confirmed Meego progress: %w", err)
		}
		if input.ExpectedVersion != 0 {
			return ErrConflict
		}
		var maxSort int
		if err := tx.Model(&domain.KRProgress{}).Where("point_id = ? AND week = ?", point.ID, input.Week).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSort).Error; err != nil {
			return fmt.Errorf("find Meego progress sort order: %w", err)
		}
		digest := sha256.Sum256([]byte(point.ID + "\x00" + input.Week))
		progress := domain.KRProgress{
			ID: fmt.Sprintf("meego-%x", digest[:8]), PointID: point.ID, Week: input.Week,
			Status: input.Status, Text: input.Text, Docs: docs, Images: []domain.ImageRef{}, Source: "meego", NeedsReview: false,
			Version: 1, SortOrder: maxSort + 1, CreatedBy: input.UpdatedBy, UpdatedBy: input.UpdatedBy,
		}
		if err := tx.Create(&progress).Error; err != nil {
			var count int64
			if countErr := tx.Model(&domain.KRProgress{}).Where("id = ?", progress.ID).Count(&count).Error; countErr == nil && count > 0 {
				return ErrConflict
			}
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

// GetProgressKRByPoint resolves a point through the reusable OKR view only.
// Generic progress handlers must not read Biz-owned tag, score or Meego
// tables merely to construct an optimistic-lock conflict response.
func (s *Service) GetProgressKRByPoint(ctx context.Context, pointID, week string) (KRView, error) {
	var point domain.KRPoint
	if err := s.db.WithContext(ctx).First(&point, "id = ?", strings.TrimSpace(pointID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return KRView{}, ErrNotFound
		}
		return KRView{}, fmt.Errorf("get progress kr by point: %w", err)
	}
	return s.GetProgressKR(ctx, point.KRID, week)
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
	if err := s.db.WithContext(ctx).Where("quarter = ? AND plan_id = ''", quarter).Order("sort_order, id").Find(&objectives).Error; err != nil {
		return MeegoBatchPreview{}, fmt.Errorf("list Meego preview objectives: %w", err)
	}
	for _, objective := range objectives {
		var records []domain.KR
		if err := s.db.WithContext(ctx).Where("objective_id = ?", objective.ID).Order("sort_order, id").Find(&records).Error; err != nil {
			return MeegoBatchPreview{}, fmt.Errorf("list Meego preview krs: %w", err)
		}
		for _, record := range records {
			owners, err := s.krOwnerViews(ctx, record.ID)
			if err != nil {
				return MeegoBatchPreview{}, err
			}
			var points []domain.KRPoint
			if err := s.db.WithContext(ctx).Where("kr_id = ? AND meego_work_item_id <> ''", record.ID).Order("sort_order, id").Find(&points).Error; err != nil {
				return MeegoBatchPreview{}, fmt.Errorf("list linked Meego points: %w", err)
			}
			for _, point := range points {
				item := MeegoBatchPreviewItem{
					ObjectiveID: objective.ID, ObjectiveTitle: objective.Title,
					KRID: record.ID, KRTitle: record.Title, Owners: owners,
					PointID: point.ID, PointTitle: point.Title,
				}
				var confirmed domain.KRProgress
				confirmedErr := s.db.WithContext(ctx).Where("point_id = ? AND week = ? AND source = ?", point.ID, week, "meego").Order("sort_order, id").First(&confirmed).Error
				if confirmedErr == nil {
					item.ProgressVersion = confirmed.Version
				} else if !errors.Is(confirmedErr, gorm.ErrRecordNotFound) {
					return MeegoBatchPreview{}, fmt.Errorf("read confirmed Meego progress version: %w", confirmedErr)
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

func normalizeTags(tags []TagView) []TagView {
	result := make([]TagView, 0, len(tags))
	for _, tag := range tags {
		result = append(result, TagView{Type: strings.TrimSpace(tag.Type), Value: strings.TrimSpace(tag.Value)})
	}
	return result
}

func validateTags(tags []TagView) error {
	seen := map[string]bool{}
	structural := map[string]string{}
	for _, tag := range tags {
		if tag.Type == "" || tag.Value == "" {
			return fmt.Errorf("tags require a type and value")
		}
		key := tag.Type + "\x00" + tag.Value
		if seen[key] {
			return fmt.Errorf("tags require unique type and value pairs")
		}
		seen[key] = true
		if tag.Type != domain.TagTypeBusinessCategory && tag.Type != domain.TagTypePriority {
			continue
		}
		if previous := structural[tag.Type]; previous != "" {
			return fmt.Errorf("tag type %s must have at most one value", tag.Type)
		}
		structural[tag.Type] = tag.Value
	}
	if priority := structural[domain.TagTypePriority]; priority != "" && !domain.ValidPriorityTag(priority) {
		return fmt.Errorf("priority tag must be p0, p1, or p2")
	}
	return nil
}

func validatePointTags(tags []TagView) error {
	if err := validateTags(tags); err != nil {
		return err
	}
	for _, tag := range tags {
		if tag.Type == domain.TagTypeBusinessCategory || tag.Type == domain.TagTypePriority {
			return fmt.Errorf("point tags cannot use structural tag type %s", tag.Type)
		}
	}
	return nil
}

func validateReplaceInput(id string, input ReplaceKRInput, includeBiz bool) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(input.Title) == "" {
		return fmt.Errorf("kr id and title are required")
	}
	if input.ExpectedVersion < 0 {
		return fmt.Errorf("expected_version must be non-negative")
	}
	seen := map[string]bool{}
	for _, metric := range input.Metrics {
		if strings.TrimSpace(metric.ID) == "" || strings.TrimSpace(metric.Text) == "" || !domain.ValidLight(metric.Light) || seen[metric.ID] {
			return fmt.Errorf("metrics require unique ids, text, and a valid light")
		}
		seen[metric.ID] = true
	}
	for _, point := range input.Points {
		if strings.TrimSpace(point.ID) == "" || seen[point.ID] {
			return fmt.Errorf("points require unique ids")
		}
		seen[point.ID] = true
		if point.Kind != "" && !domain.ValidPointKind(point.Kind) {
			return fmt.Errorf("point kind is invalid")
		}
		if len(point.Entries) > 0 || len(point.PreviousEntries) > 0 {
			return fmt.Errorf("weekly progress cannot be written through the OKR definition endpoint")
		}
		if includeBiz && point.Tags != nil {
			if err := validatePointTags(point.Tags); err != nil {
				return fmt.Errorf("invalid tags for point %s: %w", point.ID, err)
			}
		}
	}
	if includeBiz {
		if err := validateTags(input.Tags); err != nil {
			return err
		}
	}
	return nil
}

func normalizeOwners(input []OwnerView) []OwnerView {
	owners := make([]OwnerView, 0, len(input))
	owners = append(owners, input...)

	result := make([]OwnerView, 0, len(owners))
	seen := map[string]struct{}{}
	for _, owner := range owners {
		owner.Email = domain.NormalizeEmail(owner.Email)
		owner.Name = strings.TrimSpace(owner.Name)
		owner.UnionID = strings.TrimSpace(owner.UnionID)
		if owner.Name == "" {
			continue
		}
		key := owner.Email
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

func (s *Service) krOwnerViews(ctx context.Context, krID string) ([]OwnerView, error) {
	var owners []domain.KROwner
	if err := s.db.WithContext(ctx).Where("kr_id = ?", krID).Order("sort_order, owner_key, person_id").Find(&owners).Error; err != nil {
		return nil, fmt.Errorf("list KR owners: %w", err)
	}
	result := make([]OwnerView, 0, len(owners))
	for _, owner := range owners {
		result = append(result, storedOwnerView(owner.Email, owner.Name, owner.UnionID))
	}
	return result, nil
}

func ownerKey(owner OwnerView) string {
	key := domain.NormalizeEmail(owner.Email)
	if key == "" {
		key = "unresolved:" + strings.TrimSpace(owner.Name)
	}
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("owner-%x", sum[:12])
}

func ownerPersonID(owner OwnerView) uint64 {
	sum := sha256.Sum256([]byte(ownerKey(owner)))
	// SQLite INTEGER is signed; keep the deterministic local identity in its
	// positive range instead of depending on driver-specific uint64 handling.
	value := binary.BigEndian.Uint64(sum[:8]) & ((uint64(1) << 63) - 1)
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
	err := s.db.WithContext(ctx).Model(&domain.WeeklyReportWeek{}).
		Where("quarter = ?", quarter).Order("week DESC").Pluck("week", &weeks).Error
	if err != nil {
		return nil, "", fmt.Errorf("list available weeks: %w", err)
	}
	previous := ""
	for _, week := range weeks {
		if previous == "" && week < selected {
			previous = week
		}
	}
	return weeks, previous, nil
}

func EnumValues() map[string]any {
	return map[string]any{"statuses": domain.Statuses, "point_kinds": domain.PointKinds, "lights": domain.Lights}
}
