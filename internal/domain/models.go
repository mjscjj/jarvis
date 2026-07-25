// Package domain contains the persistence models shared by all Jarvis modules.
//
// The core models are the canonical Go mapping of docs/00-overview.md §2.4
// (the original seven entities plus principal_profile, the decision-maker "me").
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
	ChatMode        string    `gorm:"column:chat_mode;type:varchar(16);not null"` // group | p2p | topic
	Name            *string   `gorm:"column:name;type:varchar(512)"`
	Description     *string   `gorm:"column:description;type:text"`
	BackgroundNote  *string   `gorm:"column:background_note;type:text"` // Human-curated context; capture owns Description.
	OwnerOpenID     *string   `gorm:"column:owner_open_id;type:varchar(64)"`
	External        bool      `gorm:"column:external;type:tinyint(1);not null;default:0"`
	TenantKey       *string   `gorm:"column:tenant_key;type:varchar(64)"`
	P2PTargetType   *string   `gorm:"column:p2p_target_type;type:varchar(16)"` // 私聊对端类型：user=真人，bot=服务号；群/话题为空
	ProjectID       *uint64   `gorm:"column:project_id;type:bigint unsigned;index:idx_group_project"`
	RelatedGroup    bool      `gorm:"column:related_group;type:tinyint(1);not null;default:0;index:idx_group_related_tier,priority:1"`
	Tier            string    `gorm:"column:tier;type:varchar(8);not null;default:cold;index:idx_group_tier_active,priority:1;index:idx_group_related_tier,priority:2"`
	Pinned          bool      `gorm:"column:pinned;type:tinyint(1);not null;default:0"`
	IncludeInMemory bool      `gorm:"column:include_in_memory;type:tinyint(1);not null;default:1"`
	IsKeyGroup      bool      `gorm:"column:is_key_group;type:tinyint(1);not null;default:0"`
	LastActiveAt    *int64    `gorm:"column:last_active_at;type:bigint;index:idx_group_tier_active,priority:2;index:idx_group_related_tier,priority:3"`
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
	Target             string         `gorm:"column:target;type:varchar(512);not null"` // 这件事作用的对象/主题，去重身份
	Context            string         `gorm:"column:context;type:text;not null"`        // M3 主动补全的背景（归属/链接/相关历史）
	OpenQuestions      datatypes.JSON `gorm:"column:open_questions;type:json;not null"` // 只有必须由 principal 拍板/提供的点
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
	// ManualGateRequired is sticky once M4 asks for human review. Re-evaluating a
	// supplemented Todo may improve its plan, but must not turn that prior review
	// requirement into an automatic execution path.
	ManualGateRequired bool           `gorm:"column:manual_gate_required;type:tinyint(1);not null;default:0"`
	DedupFingerprint   string         `gorm:"column:dedup_fingerprint;type:char(64);not null;uniqueIndex:uk_todo_fingerprint"`
	ContextSnapshot    datatypes.JSON `gorm:"column:context_snapshot;type:json"`  // M3 固化的背景快照（principal/群/项目/交办人/消息/记忆），M4/M5 全链路复用
	ExtractionResult   datatypes.JSON `gorm:"column:extraction_result;type:json"` // M3 抽取吐出的完整结论原文（整个 Candidate），M4 整块复用，不逐字段拆
	Resolution         datatypes.JSON `gorm:"column:resolution;type:json"`        // 项目/仓库推算轨迹（method/project_id/repos_hint/confidence/basis）
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
	TodoID          *uint64        `gorm:"column:todo_id;type:bigint unsigned;uniqueIndex:uk_task_todo"`
	Title           string         `gorm:"column:title;type:varchar(512);not null"`
	ActionType      string         `gorm:"column:action_type;type:varchar(32);not null"`
	Target          string         `gorm:"column:target;type:varchar(512);not null;default:''"`
	Background      datatypes.JSON `gorm:"column:background;type:json;not null"`
	Plan            datatypes.JSON `gorm:"column:plan;type:json;not null"`
	DecisionPayload datatypes.JSON `gorm:"column:decision_payload;type:json"`
	ConfirmedBy     string         `gorm:"column:confirmed_by;type:varchar(16);not null"`
	ConfirmedAt     time.Time      `gorm:"column:confirmed_at;type:datetime;not null"`
	ActionHash      string         `gorm:"column:action_hash;type:char(64);not null"`
	SourceType      string         `gorm:"column:source_type;type:varchar(24);not null;default:todo;uniqueIndex:uk_task_source_occurrence,priority:1"`
	SourceID        *uint64        `gorm:"column:source_id;type:bigint unsigned;uniqueIndex:uk_task_source_occurrence,priority:2"`
	OccurrenceKey   *string        `gorm:"column:occurrence_key;type:varchar(128);uniqueIndex:uk_task_source_occurrence,priority:3"`
	ExecutionMode   string         `gorm:"column:execution_mode;type:varchar(16);not null;default:standard"`
	ApprovalRef     *string        `gorm:"column:approval_ref;type:varchar(255)"`
	Status          string         `gorm:"column:status;type:varchar(24);not null;default:pending;index:idx_task_status"`
	ExecutionResult datatypes.JSON `gorm:"column:execution_result;type:json"`
	// ExecutionSupplements are M5-only human clarifications/instructions, append-only
	// and isolated from M4's Todo.context_snapshot.supplements.
	ExecutionSupplements datatypes.JSON `gorm:"column:execution_supplements;type:json"`
	AutonomyMode         string         `gorm:"column:autonomy_mode;type:varchar(16);not null;default:copilot"`
	ProjectID            *uint64        `gorm:"column:project_id;type:bigint unsigned;index:idx_task_project"`
	Version              int32          `gorm:"column:version;not null;default:0"`
	CreatedAt            time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt            time.Time      `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`

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

// PrincipalProfile is the single decision-maker ("me") whose action clues M3
// extracts. It is a single-row table keyed by the owner's Feishu open_id; the
// background/preferences here are fed into the extraction prompt so the model
// knows who the principal is, what they own, and who their leader is. Kept
// separate from Person because its semantics (self-profile, preferences, direct
// leader) differ from a chat participant.
type PrincipalProfile struct {
	ID           uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	OpenID       string    `gorm:"column:open_id;type:varchar(64);not null;uniqueIndex:uk_principal_open_id"`
	Name         string    `gorm:"column:name;type:varchar(128);not null"`
	Department   *string   `gorm:"column:department;type:varchar(255)"`
	Title        *string   `gorm:"column:title;type:varchar(128)"`
	Background   *string   `gorm:"column:background;type:text"`  // 我是谁、负责什么方向
	Preferences  *string   `gorm:"column:preferences;type:text"` // 喜好、工作/沟通偏好
	LeaderOpenID *string   `gorm:"column:leader_open_id;type:varchar(64)"`
	LeaderName   *string   `gorm:"column:leader_name;type:varchar(128)"`
	CreatedAt    time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt    time.Time `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (PrincipalProfile) TableName() string { return "principal_profile" }

// ManagedResource is a manually maintained reference (doc/link/repo/note) that
// the owner curates from the admin UI. Unlike Resource (which capture derives
// automatically from messages), this table is human-owned and can be optionally
// linked to a Person, a Project, and/or the principal ("me") so the extraction
// tools can surface the right background material on demand.
type ManagedResource struct {
	ID            uint64    `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	Title         string    `gorm:"column:title;type:varchar(512);not null"`
	ResourceType  string    `gorm:"column:resource_type;type:enum('doc','link','repo','note','other');not null;default:link;index:idx_managed_resource_type"`
	URL           *string   `gorm:"column:url;type:varchar(1024)"`
	Description   *string   `gorm:"column:description;type:text"`
	PersonID      *uint64   `gorm:"column:person_id;type:bigint unsigned;index:idx_managed_resource_person"`
	ProjectID     *uint64   `gorm:"column:project_id;type:bigint unsigned;index:idx_managed_resource_project"`
	LinkPrincipal bool      `gorm:"column:link_principal;type:tinyint(1);not null;default:0;index:idx_managed_resource_principal"`
	IsActive      bool      `gorm:"column:is_active;type:tinyint(1);not null;default:1;index:idx_managed_resource_active"`
	CreatedAt     time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt     time.Time `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`

	Person  *Person  `gorm:"foreignKey:PersonID;constraint:OnDelete:SET NULL"`
	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (ManagedResource) TableName() string { return "managed_resource" }

// DailyDigest 是「每日进度总结」的落库缓存：一天一个 scope 一行，重算 upsert 覆盖
// （不留历史版本）。scope=person 时 scope_id 是 principal open_id；scope=group 时
// scope_id 是 feishu_group.id 的字符串。digest_date 是自然日（本地时区 00:00）。
// 生成是异步的，status 走 pending→generating→done/failed 状态机。
type DailyDigest struct {
	ID             uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	Scope          string         `gorm:"column:scope;type:varchar(16);not null;uniqueIndex:uk_scope_date,priority:1"`    // person / group
	ScopeID        string         `gorm:"column:scope_id;type:varchar(64);not null;uniqueIndex:uk_scope_date,priority:2"` // person=principal open_id；group=feishu_group.id 字符串
	DigestDate     datatypes.Date `gorm:"column:digest_date;type:date;not null;uniqueIndex:uk_scope_date,priority:3"`     // 自然日（本地时区）
	Summary        string         `gorm:"column:summary;type:mediumtext"`                                                 // 生成的一段中文进度总结
	Status         string         `gorm:"column:status;type:varchar(16);not null;default:pending"`                        // pending / generating / done / failed
	TriggerType    string         `gorm:"column:trigger_type;type:varchar(16);not null;default:manual"`                   // manual / schedule
	SourceCount    int            `gorm:"column:source_count;type:int;not null;default:0"`                                // 所有成功纳入的证据条数
	SourceCoverage datatypes.JSON `gorm:"column:source_coverage;type:json"`                                               // 各数据源 status/count/note
	Engine         string         `gorm:"column:engine;type:varchar(16);not null"`                                        // codex
	ErrorDetail    *string        `gorm:"column:error_detail;type:text"`                                                  // 失败原因（fail 时）
	StartedAt      *time.Time     `gorm:"column:started_at;type:datetime"`                                                // 本轮开始生成时刻
	CutoffAt       *time.Time     `gorm:"column:cutoff_at;type:datetime"`                                                 // 本轮证据截止时刻
	GeneratedAt    *time.Time     `gorm:"column:generated_at;type:datetime"`                                              // 生成完成时刻
	CreatedAt      time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt      time.Time      `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (DailyDigest) TableName() string { return "daily_digest" }

// ScheduledTask is a durable time trigger. Each occurrence materializes a Task
// for M5; it never executes the instruction itself. ContextSnapshot freezes the
// background available when the schedule was created.
type ScheduledTask struct {
	ID              uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`
	DispatchKind    string         `gorm:"column:dispatch_kind;type:varchar(24);not null;default:create_task;index:idx_scheduled_task_dispatch"`
	SubjectType     *string        `gorm:"column:subject_type;type:varchar(24)"`
	SubjectID       *uint64        `gorm:"column:subject_id;type:bigint unsigned;index:idx_scheduled_task_subject"`
	SourceRunID     *uint64        `gorm:"column:source_run_id;type:bigint unsigned;uniqueIndex:uk_scheduled_task_source_run"`
	DispatchPayload datatypes.JSON `gorm:"column:dispatch_payload;type:json"`
	Title           string         `gorm:"column:title;type:varchar(512);not null"`
	ActionType      string         `gorm:"column:action_type;type:varchar(32);not null;default:agent_task"`
	Instruction     string         `gorm:"column:instruction;type:mediumtext;not null"`
	ContextSnapshot datatypes.JSON `gorm:"column:context_snapshot;type:json;not null"`
	ScheduleType    string         `gorm:"column:schedule_type;type:varchar(16);not null"` // once / daily / interval
	DailyTime       *string        `gorm:"column:daily_time;type:char(5)"`                 // HH:mm in server local timezone
	IntervalMinutes *int           `gorm:"column:interval_minutes;type:int"`
	RunAt           *time.Time     `gorm:"column:run_at;type:datetime"`
	NextRunAt       time.Time      `gorm:"column:next_run_at;type:datetime;not null;index:idx_scheduled_task_due,priority:3"`
	Enabled         bool           `gorm:"column:enabled;type:tinyint(1);not null;index:idx_scheduled_task_due,priority:1"`
	Status          string         `gorm:"column:status;type:varchar(16);not null;default:active;index:idx_scheduled_task_due,priority:2"` // binding / active / running / completed
	LastRunStatus   *string        `gorm:"column:last_run_status;type:varchar(16)"`                                                        // done / failed
	LastTaskID      *uint64        `gorm:"column:last_task_id;type:bigint unsigned;index:idx_scheduled_task_last_task"`
	LastResult      *string        `gorm:"column:last_result;type:mediumtext"`
	LastErrorDetail *string        `gorm:"column:last_error_detail;type:text"`
	LastStartedAt   *time.Time     `gorm:"column:last_started_at;type:datetime"`
	LastFinishedAt  *time.Time     `gorm:"column:last_finished_at;type:datetime"`
	CreatedAt       time.Time      `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt       time.Time      `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (ScheduledTask) TableName() string { return "scheduled_task" }

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
		&PrincipalProfile{},
		&ManagedResource{},
		&DailyDigest{},
		&ScheduledTask{},
	}
}
