// Package domain defines the persisted data owned by the optional OKR module.
// It deliberately stores no foreign keys into Jarvis world entities; semantic
// projection is Agent-owned and runs through generic tools.
package domain

import "time"

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

// WeeklyReportWeek is the explicit lifecycle anchor for one reporting week.
// Opening a week only makes the empty time bucket discoverable; it does not
// copy progress, send reminders or trigger another workflow.
type WeeklyReportWeek struct {
	Quarter  string    `gorm:"primaryKey;size:16"`
	Week     string    `gorm:"primaryKey;size:16"`
	OpenedBy string    `gorm:"not null;default:''"`
	OpenedAt time.Time `gorm:"not null"`
}

func (WeeklyReportWeek) TableName() string { return "okr_workspace_week" }

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

// OAuthState is a short-lived, single-use CSRF token for the browser login
// round-trip. Only a hash is persisted.
type OAuthState struct {
	StateHash string    `gorm:"primaryKey;size:64"`
	ReturnTo  string    `gorm:"not null;default:'/#/okr'"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time `gorm:"not null"`
}

func (OAuthState) TableName() string { return "okr_workspace_oauth_state" }

// AuthSession stores an opaque browser session. Feishu access and refresh
// tokens are deliberately not retained because Emily only needs identity.
type AuthSession struct {
	TokenHash  string    `gorm:"primaryKey;size:64"`
	OpenID     string    `gorm:"not null;index"`
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
	return append(CoreModels(), WeeklyReportModels()...)
}

// CoreModels are owned by the OKR module. KRPoint is the stable decomposition
// definition; its week-specific updates are owned by WeeklyReportModels.
func CoreModels() []any {
	return []any{&Objective{}, &KR{}, &KRMetric{}, &KRPoint{}, &KRTag{}, &KROwner{}}
}

// IdentityModels are machine-local OAuth and login state. They belong to the
// runtime database and must never be committed with OKR product data.
func IdentityModels() []any {
	return []any{&OAuthState{}, &AuthSession{}}
}

// WeeklyReportModels are owned by the weekly-report module. Existing table
// names are intentionally preserved so enabling the split never rewrites or
// loses Emily's historical data.
func WeeklyReportModels() []any {
	return []any{&WeeklyReportWeek{}, &WeeklyKRCore{}, &KRProgress{}, &PageComment{}, &MeegoSyncSnapshot{}, &ReminderBatch{}}
}
