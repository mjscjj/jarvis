// Package domain defines the persisted data owned by the optional OKR module.
// It deliberately stores no foreign keys into Jarvis world entities; semantic
// projection is Agent-owned and runs through generic tools.
package domain

import (
	"time"

	"jarvis/internal/datatypes"
)

type Status string

const (
	StatusNotStarted Status = "not_started"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusAtRisk     Status = "at_risk"
	StatusDelayed    Status = "delayed"
	StatusBlocked    Status = "blocked"
)

var Statuses = []Status{
	StatusNotStarted, StatusInProgress, StatusDone,
	StatusAtRisk, StatusDelayed, StatusBlocked,
}

func ValidStatus(value Status) bool {
	for _, candidate := range Statuses {
		if candidate == value {
			return true
		}
	}
	return false
}

type PointKind string

const (
	PointKindStrategy PointKind = "strategy"
	PointKindProduct  PointKind = "product"
)

var PointKinds = []PointKind{PointKindStrategy, PointKindProduct}

func ValidPointKind(value PointKind) bool {
	return value == PointKindStrategy || value == PointKindProduct
}

type Light string

const (
	LightGreen  Light = "green"
	LightYellow Light = "yellow"
	LightRed    Light = "red"
)

var Lights = []Light{LightGreen, LightYellow, LightRed}

func ValidLight(value Light) bool {
	return value == "" || value == LightGreen || value == LightYellow || value == LightRed
}

const (
	TagTypeBusinessCategory = "business_category"
	TagTypePriority         = "priority"
)

func ValidPriorityTag(value string) bool {
	return value == "p0" || value == "p1" || value == "p2"
}

type Objective struct {
	ID        string    `gorm:"primaryKey;size:64"`
	Title     string    `gorm:"not null"`
	Quarter   string    `gorm:"not null;index"`
	SortOrder int       `gorm:"not null;default:0"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (Objective) TableName() string { return "okr_workspace_objective" }

// OKRPlan is a quarterly planning draft. Its structured content is consumed by
// the OKR plan page, while the official Objective/KR tables remain the source
// for committed OKR definitions.
type OKRPlan struct {
	ID        string         `gorm:"primaryKey;size:64"`
	Quarter   string         `gorm:"not null;index"`
	Title     string         `gorm:"not null"`
	Content   datatypes.JSON `gorm:"not null;type:text"`
	Version   int32          `gorm:"not null;default:0"`
	CreatedBy string         `gorm:"not null;default:''"`
	UpdatedBy string         `gorm:"not null;default:''"`
	CreatedAt time.Time      `gorm:"not null"`
	UpdatedAt time.Time      `gorm:"not null"`
}

func (OKRPlan) TableName() string { return "okr_workspace_plan" }

type KR struct {
	ID          string    `gorm:"primaryKey;size:64"`
	ObjectiveID string    `gorm:"not null;index"`
	Title       string    `gorm:"not null"`
	MetricNote  string    `gorm:"not null;default:''"`
	SortOrder   int       `gorm:"not null;default:0"`
	Version     int32     `gorm:"not null;default:0"`
	CreatedBy   string    `gorm:"not null;default:''"`
	UpdatedBy   string    `gorm:"not null;default:''"`
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

func (KR) TableName() string { return "okr_workspace_kr" }

type KRMetric struct {
	ID        string     `gorm:"primaryKey;size:64"`
	KRID      string     `gorm:"not null;index"`
	Text      string     `gorm:"not null"`
	Light     Light      `gorm:"not null;default:''"`
	Images    []ImageRef `gorm:"serializer:json;type:text"`
	SortOrder int        `gorm:"not null;default:0"`
}

func (KRMetric) TableName() string { return "okr_workspace_metric" }

type KRPoint struct {
	ID              string    `gorm:"primaryKey;size:64"`
	KRID            string    `gorm:"not null;index"`
	Kind            PointKind `gorm:"not null"`
	Title           string    `gorm:"not null"`
	MeegoWorkItemID string    `gorm:"not null;default:'';index"`
	MeegoURL        string    `gorm:"not null;default:''"`
	SortOrder       int       `gorm:"not null;default:0"`
}

func (KRPoint) TableName() string { return "okr_workspace_point" }

type KRProgress struct {
	ID          string     `gorm:"primaryKey;size:64"`
	PointID     string     `gorm:"not null;index;index:idx_progress_point_week"`
	Week        string     `gorm:"not null;index;index:idx_progress_point_week"`
	Version     int32      `gorm:"not null;default:0"`
	Status      Status     `gorm:"not null"`
	Text        string     `gorm:"not null"`
	Docs        []DocLink  `gorm:"serializer:json;type:text"`
	Images      []ImageRef `gorm:"serializer:json;type:text"`
	Source      string     `gorm:"not null;default:'manual'"`
	NeedsReview bool       `gorm:"not null;default:false"`
	SortOrder   int        `gorm:"not null;default:0"`
	CreatedBy   string     `gorm:"not null;default:''"`
	UpdatedBy   string     `gorm:"not null;default:''"`
	CreatedAt   time.Time  `gorm:"not null"`
	UpdatedAt   time.Time  `gorm:"not null"`
}

func (KRProgress) TableName() string { return "okr_workspace_progress" }

// FollowUpStatus is deliberately narrower than weekly KR progress. A tracker
// row is either not started, in progress, done or abandoned; risk judgements
// remain in the richer OKR progress model instead of leaking into this small
// coordination surface.
type FollowUpStatus string

const (
	FollowUpStatusNotStarted FollowUpStatus = "not_started"
	FollowUpStatusInProgress FollowUpStatus = "in_progress"
	FollowUpStatusDone       FollowUpStatus = "done"
	FollowUpStatusAbandoned  FollowUpStatus = "abandoned"
)

func ValidFollowUpStatus(value FollowUpStatus) bool {
	return value == FollowUpStatusNotStarted || value == FollowUpStatusInProgress || value == FollowUpStatusDone || value == FollowUpStatusAbandoned
}

type FollowUpOwner struct {
	OpenID string `json:"open_id"`
	Name   string `json:"name"`
}

// FollowUpItem is the Biz-OKR-owned source of truth for Review follow-up
// rows. Owners stay as one JSON value because the product only reads and edits
// the row as a whole; there is no owner-indexed query that would justify a
// second table and a multi-write protocol.
type FollowUpItem struct {
	ID            string          `gorm:"primaryKey;size:96"`
	Quarter       string          `gorm:"not null;index:idx_follow_up_scope,priority:1;size:16"`
	Week          string          `gorm:"not null;index:idx_follow_up_scope,priority:2;size:16"`
	Version       int32           `gorm:"not null;default:0"`
	Topic         string          `gorm:"not null;type:text"`
	Owners        []FollowUpOwner `gorm:"serializer:json;type:text"`
	Status        FollowUpStatus  `gorm:"not null;size:24"`
	AssignDate    string          `gorm:"not null;default:'';size:10"`
	Update        string          `gorm:"not null;type:text;default:''"`
	SourceKey     string          `gorm:"not null;default:'';index"`
	SourcePayload datatypes.JSON  `gorm:"not null;type:text"`
	SortOrder     int             `gorm:"not null;default:0"`
	CreatedBy     string          `gorm:"not null;default:''"`
	UpdatedBy     string          `gorm:"not null;default:''"`
	CreatedAt     time.Time       `gorm:"not null"`
	UpdatedAt     time.Time       `gorm:"not null"`
}

func (FollowUpItem) TableName() string { return "okr_workspace_follow_up" }

type WeekTemplateKey string

const (
	WeekTemplateClassic    WeekTemplateKey = "classic"
	WeekTemplateOKRPreview WeekTemplateKey = "okr_weekly_preview_v1"
)

func ValidWeekTemplateKey(value WeekTemplateKey) bool {
	return value == WeekTemplateClassic || value == WeekTemplateOKRPreview
}

// WeeklyReportWeek is the explicit lifecycle anchor for one reporting week.
// Opening a week only makes the empty time bucket discoverable; it does not
// copy progress, send reminders or trigger another workflow.
//
// TemplateKey is deliberately outside the primary key: one week belongs to
// exactly one template, so a given week is either a normal weekly report or an
// OKR Review, never both. That is the intended product rule — the two ceremonies
// do not run in the same week. It is also why KRProgress and WeeklyScore stay
// keyed by week alone and carry no template dimension.
type WeeklyReportWeek struct {
	Quarter     string          `gorm:"primaryKey;size:16"`
	Week        string          `gorm:"primaryKey;size:16"`
	TemplateKey WeekTemplateKey `gorm:"not null;default:'classic'"`
	OpenedBy    string          `gorm:"not null;default:''"`
	OpenedAt    time.Time       `gorm:"not null"`
}

func (WeeklyReportWeek) TableName() string { return "okr_workspace_week" }

type WeeklyScoreTargetKind string

const (
	WeeklyScoreTargetKR    WeeklyScoreTargetKind = "kr"
	WeeklyScoreTargetPoint WeeklyScoreTargetKind = "point"
)

func ValidWeeklyScoreTargetKind(value WeeklyScoreTargetKind) bool {
	return value == WeeklyScoreTargetKR || value == WeeklyScoreTargetPoint
}

// WeeklyScore is one manually assigned score for a stable KR or one of its
// strategy/product points in an explicitly opened preview week.
type WeeklyScore struct {
	Quarter    string                `gorm:"primaryKey;size:16"`
	Week       string                `gorm:"primaryKey;size:16"`
	TargetKind WeeklyScoreTargetKind `gorm:"primaryKey;size:16"`
	TargetID   string                `gorm:"primaryKey;size:64"`
	Score      float64               `gorm:"not null"`
	Version    int32                 `gorm:"not null;default:0"`
	UpdatedBy  string                `gorm:"not null"`
	CreatedAt  time.Time             `gorm:"not null"`
	UpdatedAt  time.Time             `gorm:"not null"`
}

func (WeeklyScore) TableName() string { return "okr_workspace_weekly_score" }

// WeeklyKRCore stores the editable core-data presentation for one KR in one
// reporting week. The stable metric definitions still belong to the OKR
// module; this row is created only after somebody edits the weekly values.
type WeeklyKRCore struct {
	KRID       string         `gorm:"primaryKey;size:64"`
	Week       string         `gorm:"primaryKey;size:16"`
	Version    int32          `gorm:"not null;default:0"`
	MetricNote string         `gorm:"not null;default:''"`
	Metrics    []WeeklyMetric `gorm:"serializer:json;type:text"`
	CreatedBy  string         `gorm:"not null;default:''"`
	UpdatedBy  string         `gorm:"not null;default:''"`
	CreatedAt  time.Time      `gorm:"not null"`
	UpdatedAt  time.Time      `gorm:"not null"`
}

func (WeeklyKRCore) TableName() string { return "okr_workspace_weekly_kr_core" }

type WeeklyMetric struct {
	ID     string     `json:"id"`
	Text   string     `json:"text"`
	Light  Light      `json:"light,omitempty"`
	Images []ImageRef `json:"images"`
}

// MeegoSyncSnapshot is a read-only observation cache. It records what Emily
// last saw in Meego and the poll health without changing KR or progress rows.
type MeegoSyncSnapshot struct {
	PointID         string     `gorm:"primaryKey;size:64"`
	WorkItemID      string     `gorm:"not null;default:'';index"`
	Week            string     `gorm:"not null;default:'';index"`
	LocalStatus     string     `gorm:"not null;default:''"`
	LocalProgress   string     `gorm:"not null;default:''"`
	RemoteTitle     string     `gorm:"not null;default:''"`
	RemoteStatus    string     `gorm:"not null;default:''"`
	RemoteProgress  string     `gorm:"not null;default:''"`
	RemoteUpdatedAt string     `gorm:"not null;default:''"`
	StatusChanged   bool       `gorm:"not null;default:false"`
	ProgressChanged bool       `gorm:"not null;default:false"`
	NeedsReview     bool       `gorm:"not null;default:false"`
	Risk            bool       `gorm:"not null;default:false"`
	LastAttemptAt   time.Time  `gorm:"not null"`
	LastSuccessAt   *time.Time `gorm:"index"`
	LastError       string     `gorm:"not null;default:''"`
	CreatedAt       time.Time  `gorm:"not null"`
	UpdatedAt       time.Time  `gorm:"not null"`
}

func (MeegoSyncSnapshot) TableName() string { return "okr_workspace_meego_snapshot" }

type DocLink struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type ImageRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Width int    `json:"width,omitempty"`
}

type KRTag struct {
	KRID  string `gorm:"primaryKey;size:64"`
	Type  string `gorm:"primaryKey;size:32"`
	Value string `gorm:"primaryKey;size:64"`
}

func (KRTag) TableName() string { return "okr_workspace_tag" }

// PointTag labels one concrete strategy/product decomposition. Structural
// business and priority tags remain on the parent KR.
type PointTag struct {
	PointID string `gorm:"primaryKey;size:64"`
	Type    string `gorm:"primaryKey;size:32"`
	Value   string `gorm:"primaryKey;size:64"`
}

func (PointTag) TableName() string { return "okr_workspace_point_tag" }

// PointOwner is the persisted owner of one concrete strategy/product
// decomposition. It mirrors KROwner so both levels resolve identities the same
// way; OpenID stays empty until a human or Agent resolves a real Feishu user.
type PointOwner struct {
	PointID   string `gorm:"primaryKey;size:64"`
	PersonID  uint64 `gorm:"primaryKey;index:idx_okr_workspace_point_owner_person_id"`
	OwnerKey  string `gorm:"not null;default:'';size:64;index:idx_okr_workspace_point_owner_key"`
	OpenID    string `gorm:"not null;default:'';index:idx_okr_workspace_point_owner_open_id"`
	Name      string `gorm:"not null"`
	SortOrder int    `gorm:"not null;default:0"`
}

func (PointOwner) TableName() string { return "okr_workspace_point_owner" }

// KROwner is the only persisted owner source. PersonID is a stable local key;
// OpenID stays empty until a human or Agent resolves a real Feishu identity.
type KROwner struct {
	KRID      string `gorm:"primaryKey;size:64"`
	PersonID  uint64 `gorm:"primaryKey;index:idx_okr_workspace_kr_owner_person_id"`
	OwnerKey  string `gorm:"not null;default:'';size:64;index:idx_okr_workspace_owner_key"`
	OpenID    string `gorm:"not null;default:'';index:idx_okr_workspace_owner_open_id"`
	Name      string `gorm:"not null"`
	SortOrder int    `gorm:"not null;default:0"`
}

func (KROwner) TableName() string { return "okr_workspace_kr_owner" }

// PageComment is a lightweight document-style discussion thread scoped to a
// weekly OKR page. ParentID is empty for a root comment and points to another
// PageComment for a reply. Target fields leave room for comments anchored to a
// KR or point without coupling the discussion lifecycle to those aggregates.
type PageComment struct {
	ID              string `gorm:"primaryKey;size:64"`
	Quarter         string `gorm:"not null;index:idx_page_comment_scope,priority:1"`
	Week            string `gorm:"not null;index:idx_page_comment_scope,priority:2"`
	ParentID        string `gorm:"not null;default:'';index"`
	TargetType      string `gorm:"not null;size:24;default:'page'"`
	TargetID        string `gorm:"not null;size:96;default:''"`
	TargetTitle     string `gorm:"not null;default:''"`
	SelectedText    string `gorm:"not null;type:text;default:''"`
	SelectionStart  int    `gorm:"not null;default:0"`
	SelectionEnd    int    `gorm:"not null;default:0"`
	SelectionPrefix string `gorm:"not null;type:text;default:''"`
	SelectionSuffix string `gorm:"not null;type:text;default:''"`
	AuthorOpenID    string `gorm:"not null;default:'';index"`
	AuthorUnionID   string `gorm:"not null;default:'';index"`
	AuthorName      string `gorm:"not null;default:''"`
	Content         string `gorm:"not null;type:text"`
	// Todo promotes a meeting comment into the weekly follow-up summary. It
	// remains a comment attribute so there is only one source of truth.
	Todo      bool      `gorm:"not null;default:false"`
	Resolved  bool      `gorm:"not null;default:false"`
	CreatedAt time.Time `gorm:"not null;index"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (PageComment) TableName() string { return "okr_workspace_comment" }

// AuthSession stores an opaque browser session. It carries identity only; the
// Feishu tokens of that same login live in the auth package's on-disk token
// store, not in this table.
type AuthSession struct {
	TokenHash  string    `gorm:"primaryKey;size:64"`
	OpenID     string    `gorm:"not null;index"`
	UnionID    string    `gorm:"not null;default:'';index"`
	Name       string    `gorm:"not null"`
	AvatarURL  string    `gorm:"not null;default:''"`
	Email      string    `gorm:"not null;default:''"`
	ExpiresAt  time.Time `gorm:"not null;index"`
	CreatedAt  time.Time `gorm:"not null"`
	LastSeenAt time.Time `gorm:"not null;index"`
}

func (AuthSession) TableName() string { return "okr_workspace_auth_session" }

// ReminderBatch is an immutable, review-only snapshot produced by either the
// Monday scheduler or an explicit preview refresh. It deliberately contains no
// delivery state: Emily cannot send a batch from this model.
type ReminderBatch struct {
	ID             string     `gorm:"primaryKey;size:64"`
	Quarter        string     `gorm:"not null;index:idx_reminder_batch_scope,priority:1"`
	Week           string     `gorm:"not null;index:idx_reminder_batch_scope,priority:2"`
	Trigger        string     `gorm:"not null;size:24"`
	Status         string     `gorm:"not null;size:24;index"`
	RecipientCount int        `gorm:"not null;default:0"`
	MissingCount   int        `gorm:"not null;default:0"`
	SummaryJSON    string     `gorm:"not null;type:text;default:'{}'"`
	RecipientsJSON string     `gorm:"not null;type:text;default:'[]'"`
	LastError      string     `gorm:"not null;default:''"`
	StartedAt      time.Time  `gorm:"not null"`
	FinishedAt     *time.Time `gorm:"index"`
	CreatedAt      time.Time  `gorm:"not null"`
	UpdatedAt      time.Time  `gorm:"not null"`
}

func (ReminderBatch) TableName() string { return "okr_workspace_reminder_batch" }

func Models() []any {
	return append(CoreModels(), BizModels()...)
}

// CoreModels are owned by the reusable OKR module. Existing table names stay
// unchanged: splitting module ownership must not copy or rewrite user data.
func CoreModels() []any {
	return []any{&Objective{}, &KR{}, &KRMetric{}, &KRPoint{}, &KROwner{}, &PointOwner{}, &WeeklyReportWeek{}, &WeeklyKRCore{}, &KRProgress{}}
}

// IdentityModels are machine-local browser sessions. Pending device grants stay
// in process memory, and issued Feishu tokens go to local files, so neither
// enters either database.
func IdentityModels() []any {
	return []any{&AuthSession{}}
}

// BizModels are the organization-specific wrapper around the reusable OKR
// domain. Existing table names are intentionally preserved so enabling the
// split never rewrites or loses historical data.
func BizModels() []any {
	return []any{&OKRPlan{}, &KRTag{}, &PointTag{}, &FollowUpItem{}, &WeeklyScore{}, &PageComment{}, &MeegoSyncSnapshot{}, &ReminderBatch{}}
}
