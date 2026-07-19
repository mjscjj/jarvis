// Package domain contains the persistence models shared by all Jarvis modules.
//
// These seven models are the canonical Go mapping of docs/00-overview.md §2.4.
// MySQL is the source of truth; JSON fields intentionally remain untyped at this
// layer so each owning module can decode them into its own validated contract.
package domain

import (
	"time"

	"gorm.io/datatypes"
)

// Project is the long-lived background for a project the owner participates in.
type Project struct {
	ID           uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	Code         *string        `gorm:"column:code;type:varchar(64);uniqueIndex:uk_project_code"`
	Name         string         `gorm:"column:name;type:varchar(255);not null"`
	Role         string         `gorm:"column:role;type:enum('owner','participant');not null;index:idx_project_role"`
	Status       string         `gorm:"column:status;type:enum('planning','active','paused','archived','done');not null;default:active;index:idx_project_status"`
	Priority     uint8          `gorm:"column:priority;type:tinyint unsigned;not null;default:3;check:ck_project_priority,priority between 1 and 5"`
	Description  *string        `gorm:"column:description;type:text"`
	Repos        datatypes.JSON `gorm:"column:repos;type:json"`
	TechStack    datatypes.JSON `gorm:"column:tech_stack;type:json"`
	KeyDecisions datatypes.JSON `gorm:"column:key_decisions;type:json"`
	Timeline     datatypes.JSON `gorm:"column:timeline;type:json"`
	Notes        *string        `gorm:"column:notes;type:text"`
	Mem0SyncedAt *time.Time     `gorm:"column:mem0_synced_at;type:datetime"`
	CreatedAt    time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt    time.Time      `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (Project) TableName() string { return "project" }

// Group is a Feishu group chat or p2p conversation. The physical name avoids
// the reserved SQL keyword GROUP.
type Group struct {
	ID              uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	ChatID          string    `gorm:"column:chat_id;type:varchar(64);not null;uniqueIndex:uk_group_chat_id"`
	ChatMode        string    `gorm:"column:chat_mode;type:varchar(16);not null"`
	Name            *string   `gorm:"column:name;type:varchar(512)"`
	Description     *string   `gorm:"column:description;type:text"`
	OwnerOpenID     *string   `gorm:"column:owner_open_id;type:varchar(64)"`
	External        bool      `gorm:"column:external;type:tinyint(1);not null;default:0"`
	TenantKey       *string   `gorm:"column:tenant_key;type:varchar(64)"`
	ProjectID       *uint64   `gorm:"column:project_id;type:bigint unsigned;index:idx_group_project"`
	Tier            string    `gorm:"column:tier;type:varchar(8);not null;default:cold;index:idx_group_tier_active,priority:1"`
	Pinned          bool      `gorm:"column:pinned;type:tinyint(1);not null;default:0"`
	IncludeInMemory bool      `gorm:"column:include_in_memory;type:tinyint(1);not null;default:1"`
	IsKeyGroup      bool      `gorm:"column:is_key_group;type:tinyint(1);not null;default:0"`
	LastActiveAt    *int64    `gorm:"column:last_active_at;type:bigint;index:idx_group_tier_active,priority:2"`
	CreatedAt       time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt       time.Time `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`

	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (Group) TableName() string { return "feishu_group" }

// Person is a manually maintained important person, keyed by Feishu open_id.
type Person struct {
	ID             uint64     `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	OpenID         string     `gorm:"column:open_id;type:varchar(64);not null;uniqueIndex:uk_person_open_id"`
	UnionID        *string    `gorm:"column:union_id;type:varchar(64)"`
	FeishuUserID   *string    `gorm:"column:feishu_user_id;type:varchar(64)"`
	Name           string     `gorm:"column:name;type:varchar(128);not null"`
	EnName         *string    `gorm:"column:en_name;type:varchar(128)"`
	AvatarURL      *string    `gorm:"column:avatar_url;type:varchar(512)"`
	Department     *string    `gorm:"column:department;type:varchar(255)"`
	Title          *string    `gorm:"column:title;type:varchar(128)"`
	Role           string     `gorm:"column:role;type:enum('leader','key','colleague','other');not null;index:idx_person_role"`
	PriorityWeight float64    `gorm:"column:priority_weight;type:decimal(3,2);not null;check:ck_person_priority_weight,priority_weight between 0 and 1"`
	Relation       *string    `gorm:"column:relation;type:varchar(255)"`
	CommStyle      *string    `gorm:"column:comm_style;type:text"`
	P2PChatID      *string    `gorm:"column:p2p_chat_id;type:varchar(64)"`
	Notes          *string    `gorm:"column:notes;type:text"`
	IsActive       bool       `gorm:"column:is_active;type:tinyint(1);not null;default:1"`
	Mem0SyncedAt   *time.Time `gorm:"column:mem0_synced_at;type:datetime"`
	CreatedAt      time.Time  `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (Person) TableName() string { return "person" }

// Todo is an extracted action clue. M3 creates it and M4 owns scoring/routing.
type Todo struct {
	ID                 uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	Title              string         `gorm:"column:title;type:varchar(512);not null"`
	Description        string         `gorm:"column:description;type:text;not null"`
	ActionType         string         `gorm:"column:action_type;type:varchar(32);not null"`
	Slots              datatypes.JSON `gorm:"column:slots;type:json;not null"`
	CommitmentStrength string         `gorm:"column:commitment_strength;type:varchar(16);not null"`
	SourceMessageIDs   datatypes.JSON `gorm:"column:source_message_ids;type:json;not null"`
	SourceQuote        string         `gorm:"column:source_quote;type:text;not null"`
	GroupID            *uint64        `gorm:"column:group_id;type:bigint unsigned;index:idx_todo_group"`
	ProjectID          *uint64        `gorm:"column:project_id;type:bigint unsigned;index:idx_todo_project"`
	AssignerOpenID     *string        `gorm:"column:assigner_open_id;type:varchar(64)"`
	IsLeaderAssigned   bool           `gorm:"column:is_leader_assigned;type:tinyint(1);not null;default:0;index:idx_todo_leader_status,priority:1"`
	DueAt              *time.Time     `gorm:"column:due_at;type:datetime"`
	Status             string         `gorm:"column:status;type:varchar(24);not null;default:extracted;index:idx_todo_status;index:idx_todo_leader_status,priority:2"`
	Confidence         *float64       `gorm:"column:confidence;type:decimal(4,3)"`
	Risk               *float64       `gorm:"column:risk;type:decimal(4,3)"`
	Route              *string        `gorm:"column:route;type:varchar(16)"`
	MissingInfo        datatypes.JSON `gorm:"column:missing_info;type:json"`
	DedupFingerprint   string         `gorm:"column:dedup_fingerprint;type:char(64);not null;uniqueIndex:uk_todo_fingerprint"`
	ExtractionModel    string         `gorm:"column:extraction_model;type:varchar(64);not null"`
	PromptVersion      string         `gorm:"column:prompt_version;type:varchar(32);not null"`
	Revision           int32          `gorm:"column:revision;not null;default:1"`
	TTLAt              *time.Time     `gorm:"column:ttl_at;type:datetime"`
	Version            int32          `gorm:"column:version;not null;default:0"`
	FirstSeenAt        time.Time      `gorm:"column:first_seen_at;type:datetime;not null"`
	LastEvidenceAt     time.Time      `gorm:"column:last_evidence_at;type:datetime;not null"`
	CreatedAt          time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt          time.Time      `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`

	Group   *Group   `gorm:"foreignKey:GroupID;constraint:OnDelete:SET NULL"`
	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (Todo) TableName() string { return "todo" }

// Task is the immutable-at-confirmation executable snapshot materialized by M4.
type Task struct {
	ID              uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	TodoID          uint64         `gorm:"column:todo_id;type:bigint unsigned;not null;uniqueIndex:uk_task_todo"`
	Title           string         `gorm:"column:title;type:varchar(512);not null"`
	ActionType      string         `gorm:"column:action_type;type:varchar(32);not null"`
	Background      datatypes.JSON `gorm:"column:background;type:json;not null"`
	Plan            datatypes.JSON `gorm:"column:plan;type:json;not null"`
	Slots           datatypes.JSON `gorm:"column:slots;type:json;not null"`
	ConfirmedBy     string         `gorm:"column:confirmed_by;type:varchar(16);not null"`
	ConfirmedAt     time.Time      `gorm:"column:confirmed_at;type:datetime;not null"`
	ActionHash      string         `gorm:"column:action_hash;type:char(64);not null"`
	Status          string         `gorm:"column:status;type:varchar(16);not null;default:pending;index:idx_task_status"`
	ExecutionResult datatypes.JSON `gorm:"column:execution_result;type:json"`
	AutonomyMode    string         `gorm:"column:autonomy_mode;type:varchar(16);not null;default:copilot"`
	ProjectID       *uint64        `gorm:"column:project_id;type:bigint unsigned;index:idx_task_project"`
	Version         int32          `gorm:"column:version;not null;default:0"`
	CreatedAt       time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt       time.Time      `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`

	Todo    *Todo    `gorm:"foreignKey:TodoID;constraint:OnDelete:RESTRICT"`
	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (Task) TableName() string { return "task" }

// Resource is a first-class reference to content carried by a message.
type Resource struct {
	ID              uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	ResourceType    string    `gorm:"column:resource_type;type:varchar(24);not null;index:idx_resource_type"`
	FileKey         *string   `gorm:"column:file_key;type:varchar(128);uniqueIndex:uk_resource_msg_key,priority:2"`
	MinuteToken     *string   `gorm:"column:minute_token;type:varchar(64)"`
	DocToken        *string   `gorm:"column:doc_token;type:varchar(64)"`
	URL             *string   `gorm:"column:url;type:varchar(1024)"`
	Name            *string   `gorm:"column:name;type:varchar(512)"`
	MIMEType        *string   `gorm:"column:mime_type;type:varchar(128)"`
	SizeBytes       *int64    `gorm:"column:size_bytes;type:bigint"`
	SourceMessageID *string   `gorm:"column:source_message_id;type:varchar(64);uniqueIndex:uk_resource_msg_key,priority:1;index:idx_resource_msg"`
	GroupID         *uint64   `gorm:"column:group_id;type:bigint unsigned;index:idx_resource_group"`
	LocalPath       *string   `gorm:"column:local_path;type:varchar(1024)"`
	Downloaded      bool      `gorm:"column:downloaded;type:tinyint(1);not null;default:0"`
	ContentHash     *string   `gorm:"column:content_hash;type:char(64);index:idx_resource_content"`
	ExtractedText   *string   `gorm:"column:extracted_text;type:mediumtext"`
	CreatedAt       time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt       time.Time `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (Resource) TableName() string { return "resource" }

// ScanRecord is append-only operational history for one capture attempt.
type ScanRecord struct {
	ID              uint64     `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	ScanType        string     `gorm:"column:scan_type;type:varchar(24);not null;index:idx_scan_type_time,priority:1"`
	GroupID         *uint64    `gorm:"column:group_id;type:bigint unsigned;index:idx_scan_group_time,priority:1"`
	ChatID          *string    `gorm:"column:chat_id;type:varchar(64)"`
	WindowStart     *int64     `gorm:"column:window_start;type:bigint"`
	WindowEnd       *int64     `gorm:"column:window_end;type:bigint"`
	FetchedCount    int32      `gorm:"column:fetched_count;not null;default:0"`
	InsertedCount   int32      `gorm:"column:inserted_count;not null;default:0"`
	PageCount       int32      `gorm:"column:page_count;not null;default:0"`
	Status          string     `gorm:"column:status;type:varchar(16);not null;index:idx_scan_status"`
	ErrorType       *string    `gorm:"column:error_type;type:varchar(64)"`
	ErrorMessage    *string    `gorm:"column:error_message;type:text"`
	HighWaterBefore *int64     `gorm:"column:high_water_before;type:bigint"`
	HighWaterAfter  *int64     `gorm:"column:high_water_after;type:bigint"`
	StartedAt       time.Time  `gorm:"column:started_at;type:datetime;not null;index:idx_scan_group_time,priority:2;index:idx_scan_type_time,priority:2"`
	FinishedAt      *time.Time `gorm:"column:finished_at;type:datetime"`
	DurationMS      *int32     `gorm:"column:duration_ms"`
	CreatedAt       time.Time  `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
}

func (ScanRecord) TableName() string { return "scan_record" }

// CoreModels returns the canonical dependency-ordered migration list.
func CoreModels() []any {
	return []any{
		&Project{},
		&Group{},
		&Person{},
		&Todo{},
		&Task{},
		&Resource{},
		&ScanRecord{},
	}
}
