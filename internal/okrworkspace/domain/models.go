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
	OwnerOpenID string    `gorm:"not null;default:'';index"`
	OwnerName   string    `gorm:"not null;default:'';index"`
	Priority    string    `gorm:"not null;default:'p1'"`
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

// KROwner preserves Emily's multi-owner assignment and links it to Jarvis's
// Person entity. OwnerName/OwnerOpenID on KR remain the compact UI projection.
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
	ID              string    `gorm:"primaryKey;size:64"`
	Quarter         string    `gorm:"not null;index:idx_page_comment_scope,priority:1"`
	Week            string    `gorm:"not null;index:idx_page_comment_scope,priority:2"`
	ParentID        string    `gorm:"not null;default:'';index"`
	TargetType      string    `gorm:"not null;size:24;default:'page'"`
	TargetID        string    `gorm:"not null;size:96;default:''"`
	TargetTitle     string    `gorm:"not null;default:''"`
	SelectedText    string    `gorm:"not null;type:text;default:''"`
	SelectionStart  int       `gorm:"not null;default:0"`
	SelectionEnd    int       `gorm:"not null;default:0"`
	SelectionPrefix string    `gorm:"not null;type:text;default:''"`
	SelectionSuffix string    `gorm:"not null;type:text;default:''"`
	AuthorOpenID    string    `gorm:"not null;default:'';index"`
	AuthorName      string    `gorm:"not null;default:''"`
	Content         string    `gorm:"not null;type:text"`
	CreatedAt       time.Time `gorm:"not null;index"`
	UpdatedAt       time.Time `gorm:"not null"`
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

// ReportDraft stores only the human-edited reporting projection. KR progress
// remains the fact source and is never overwritten by draft edits.
type ReportDraft struct {
	ID         string               `gorm:"primaryKey;size:64"`
	Quarter    string               `gorm:"not null;index"`
	Week       string               `gorm:"not null;index"`
	ReportType string               `gorm:"not null;index"`
	TagType    string               `gorm:"not null;default:''"`
	TagValue   string               `gorm:"not null;default:''"`
	Title      string               `gorm:"not null"`
	Sections   []ReportDraftSection `gorm:"serializer:json;type:text"`
	Version    int32                `gorm:"not null;default:0"`
	UpdatedBy  string               `gorm:"not null;default:''"`
	CreatedAt  time.Time            `gorm:"not null"`
	UpdatedAt  time.Time            `gorm:"not null"`
}

func (ReportDraft) TableName() string { return "okr_workspace_report_draft" }

type ReportDraftSection struct {
	Kind  string            `json:"kind"`
	Title string            `json:"title"`
	Items []ReportDraftItem `json:"items"`
}

type ReportDraftItem struct {
	ID             string `json:"id"`
	ObjectiveID    string `json:"objective_id"`
	ObjectiveTitle string `json:"objective_title"`
	KRID           string `json:"kr_id"`
	KRTitle        string `json:"kr_title"`
	OwnerName      string `json:"owner_name"`
	PointID        string `json:"point_id"`
	PointTitle     string `json:"point_title"`
	Week           string `json:"week"`
	Status         string `json:"status,omitempty"`
	Source         string `json:"source"`
	Detail         string `json:"detail"`
}

type ReportDraftRevision struct {
	ID        string               `gorm:"primaryKey;size:96"`
	DraftID   string               `gorm:"not null;index:idx_report_revision,priority:1"`
	Version   int32                `gorm:"not null;index:idx_report_revision,priority:2"`
	Title     string               `gorm:"not null"`
	Sections  []ReportDraftSection `gorm:"serializer:json;type:text"`
	UpdatedBy string               `gorm:"not null;default:''"`
	CreatedAt time.Time            `gorm:"not null"`
}

func (ReportDraftRevision) TableName() string { return "okr_workspace_report_revision" }

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
	return []any{&Objective{}, &KR{}, &KRMetric{}, &KRPoint{}, &KRTag{}, &KROwner{}, &OAuthState{}, &AuthSession{}}
}

// WeeklyReportModels are owned by the weekly-report module. Existing table
// names are intentionally preserved so enabling the split never rewrites or
// loses Emily's historical data.
func WeeklyReportModels() []any {
	return []any{&KRProgress{}, &PageComment{}, &MeegoSyncSnapshot{}, &ReportDraft{}, &ReportDraftRevision{}, &ReminderBatch{}}
}
