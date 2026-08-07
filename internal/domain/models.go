// Package domain contains the persistence models shared by all Jarvis modules.
//
// The core models are the canonical Go mapping of docs/00-overview.md §2.4
// (the original seven entities plus principal_profile, the decision-maker "me").
// SQLite is the source of truth; JSON fields intentionally remain untyped at
// this layer so each owning module can decode them into its own validated contract.
package domain

import (
	"time"

	"jarvis/internal/datatypes"
)

// Project is the long-lived background for a project the owner participates in.
type Project struct {
	ID           uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	Code         *string        `gorm:"column:code;uniqueIndex:uk_project_code"`
	Name         string         `gorm:"column:name;not null"`
	Role         string         `gorm:"column:role;not null;index:idx_project_role"`
	Status       string         `gorm:"column:status;not null;default:active;index:idx_project_status"`
	Priority     uint8          `gorm:"column:priority;not null;default:3;check:ck_project_priority,priority between 1 and 5"`
	Description  *string        `gorm:"column:description"`
	Repos        datatypes.JSON `gorm:"column:repos"`
	TechStack    datatypes.JSON `gorm:"column:tech_stack"`
	KeyDecisions datatypes.JSON `gorm:"column:key_decisions"`
	Timeline     datatypes.JSON `gorm:"column:timeline"`
	Notes        *string        `gorm:"column:notes"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt    time.Time      `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (Project) TableName() string { return "project" }

// KeyMatter is a long-running thing worth remembering that is not a Project
// and not a single executable action. Its history lives in Fact rows with
// subject_type=key_matter.
type KeyMatter struct {
	ID    uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	Title string `gorm:"column:title;not null"`
	// Status is free text for humans and agents; only ClosedAt decides closure.
	Status    string     `gorm:"column:status;not null;default:''"`
	Summary   *string    `gorm:"column:summary"`
	ProjectID *uint64    `gorm:"column:project_id;index:idx_key_matter_project"`
	DueAt     *time.Time `gorm:"column:due_at;index:idx_key_matter_due"`
	ClosedAt  *time.Time `gorm:"column:closed_at;index:idx_key_matter_closed"`
	// LastProgressAt moves only when Summary actually changes.
	LastProgressAt *time.Time `gorm:"column:last_progress_at;index:idx_key_matter_last_progress"`
	// LastActiveAt is an explicit relevance signal. Ordinary profile edits do not
	// move it; callers touch the matter only after fresh evidence confirms it is
	// still worth keeping near the front of the working set.
	LastActiveAt time.Time `gorm:"column:last_active_at;not null;default:'1970-01-01 00:00:00';index:idx_key_matter_active"`
	CreatedAt    time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`

	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (KeyMatter) TableName() string { return "key_matter" }

// Group is a Feishu group chat or p2p conversation. The physical name avoids
// the reserved SQL keyword GROUP.
type Group struct {
	ID              uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ChatID          string    `gorm:"column:chat_id;not null;uniqueIndex:uk_group_chat_id"`
	ChatMode        string    `gorm:"column:chat_mode;not null"` // group | p2p | topic
	Name            *string   `gorm:"column:name"`
	Description     *string   `gorm:"column:description"`
	BackgroundNote  *string   `gorm:"column:background_note"` // Human-curated context; capture owns Description.
	OwnerOpenID     *string   `gorm:"column:owner_open_id"`
	External        bool      `gorm:"column:external;not null;default:0"`
	TenantKey       *string   `gorm:"column:tenant_key"`
	P2PTargetType   *string   `gorm:"column:p2p_target_type"` // 私聊对端类型：user=真人，bot=服务号；群/话题为空
	ProjectID       *uint64   `gorm:"column:project_id;index:idx_group_project"`
	RelatedGroup    bool      `gorm:"column:related_group;not null;default:0;index:idx_group_related_tier,priority:1"`
	Tier            string    `gorm:"column:tier;not null;default:cold;index:idx_group_tier_active,priority:1;index:idx_group_related_tier,priority:2"`
	Pinned          bool      `gorm:"column:pinned;not null;default:0"`
	IncludeInMemory bool      `gorm:"column:include_in_memory;not null;default:1"`
	IsKeyGroup      bool      `gorm:"column:is_key_group;not null;default:0"`
	LastActiveAt    *int64    `gorm:"column:last_active_at;index:idx_group_tier_active,priority:2;index:idx_group_related_tier,priority:3"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt       time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`

	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (Group) TableName() string { return "feishu_group" }

// Person is a manually maintained important person, keyed by Feishu open_id.
type Person struct {
	ID             uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	OpenID         string    `gorm:"column:open_id;not null;uniqueIndex:uk_person_open_id"`
	UnionID        *string   `gorm:"column:union_id"`
	FeishuUserID   *string   `gorm:"column:feishu_user_id"`
	Name           string    `gorm:"column:name;not null"`
	EnName         *string   `gorm:"column:en_name"`
	AvatarURL      *string   `gorm:"column:avatar_url"`
	Department     *string   `gorm:"column:department"`
	Title          *string   `gorm:"column:title"`
	Role           string    `gorm:"column:role;not null;index:idx_person_role"`
	PriorityWeight float64   `gorm:"column:priority_weight;not null;check:ck_person_priority_weight,priority_weight between 0 and 1"`
	Relation       *string   `gorm:"column:relation"`
	CommStyle      *string   `gorm:"column:comm_style"`
	P2PChatID      *string   `gorm:"column:p2p_chat_id"`
	Notes          *string   `gorm:"column:notes"`
	IsActive       bool      `gorm:"column:is_active;not null;default:1"`
	CreatedAt      time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (Person) TableName() string { return "person" }

// Todo is an extracted action clue. M3 chooses extracted or observing; extracted
// clues are mechanically materialized as Tasks.
type Todo struct {
	ID                 uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	Title              string         `gorm:"column:title;not null"`
	Description        string         `gorm:"column:description;not null"`
	ActionType         string         `gorm:"column:action_type;not null"`
	Target             string         `gorm:"column:target;not null"`         // 这件事作用的对象/主题，去重身份
	Context            string         `gorm:"column:context;not null"`        // M3 主动补全的背景（归属/链接/相关历史）
	OpenQuestions      datatypes.JSON `gorm:"column:open_questions;not null"` // 只有必须由 principal 拍板/提供的点
	CommitmentStrength string         `gorm:"column:commitment_strength;not null"`
	SourceMessageIDs   datatypes.JSON `gorm:"column:source_message_ids;not null"`
	SourceQuote        string         `gorm:"column:source_quote;not null"`
	GroupID            *uint64        `gorm:"column:group_id;index:idx_todo_group"`
	ProjectID          *uint64        `gorm:"column:project_id;index:idx_todo_project"`
	AssignerOpenID     *string        `gorm:"column:assigner_open_id"`
	IsLeaderAssigned   bool           `gorm:"column:is_leader_assigned;not null;default:0;index:idx_todo_leader_status,priority:1"`
	DueAt              *time.Time     `gorm:"column:due_at"`
	// Status is extracted while awaiting materialization, then materialized once
	// a Task exists. Observing clues stay visible without creating a Task.
	Status           string         `gorm:"column:status;not null;default:extracted;index:idx_todo_status;index:idx_todo_leader_status,priority:2"`
	DedupFingerprint string         `gorm:"column:dedup_fingerprint;not null;uniqueIndex:uk_todo_fingerprint"`
	ContextSnapshot  datatypes.JSON `gorm:"column:context_snapshot"`  // M3 固化的背景快照（principal/群/项目/交办人/消息/记忆），Task 与执行环节全链路复用
	ExtractionResult datatypes.JSON `gorm:"column:extraction_result"` // M3 抽取吐出的完整结论原文（整个 Candidate），Task 与执行环节整块复用
	Resolution       datatypes.JSON `gorm:"column:resolution"`        // 项目/仓库推算轨迹（method/project_id/repos_hint/confidence/basis）
	// Revision counts how many times this clue was re-extracted; Version is the
	// optimistic lock. They are different things and must not be merged.
	Revision       int32     `gorm:"column:revision;not null;default:1"`
	Version        int32     `gorm:"column:version;not null;default:0"`
	FirstSeenAt    time.Time `gorm:"column:first_seen_at;not null"`
	LastEvidenceAt time.Time `gorm:"column:last_evidence_at;not null;index:idx_todo_last_evidence"`
	CreatedAt      time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`

	Group   *Group   `gorm:"foreignKey:GroupID;constraint:OnDelete:SET NULL"`
	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (Todo) TableName() string { return "todo" }

// Task is the executable snapshot materialized from a Todo or another source.
// M5 owns all semantic judgment during execution.
type Task struct {
	ID         uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	TodoID     *uint64        `gorm:"column:todo_id;uniqueIndex:uk_task_todo"`
	Title      string         `gorm:"column:title;not null"`
	ActionType string         `gorm:"column:action_type;not null"`
	Target     string         `gorm:"column:target;not null;default:''"`
	Background datatypes.JSON `gorm:"column:background;not null"`
	// SourcePayload is the source-owned semantic input frozen at Task creation.
	// Todo, scheduled, manual and proactive Tasks all use this same loose JSON
	// carrier; M5 treats it as evidence, not as an immutable execution plan.
	SourcePayload   datatypes.JSON `gorm:"column:source_payload"`
	SourceType      string         `gorm:"column:source_type;not null;default:todo;uniqueIndex:uk_task_source_occurrence,priority:1"`
	SourceID        *uint64        `gorm:"column:source_id;uniqueIndex:uk_task_source_occurrence,priority:2"`
	OccurrenceKey   *string        `gorm:"column:occurrence_key;uniqueIndex:uk_task_source_occurrence,priority:3"`
	Status          string         `gorm:"column:status;not null;default:pending;index:idx_task_status"`
	ExecutionResult datatypes.JSON `gorm:"column:execution_result"`
	// Summary is where the matter itself now stands, written by M5 after a run or
	// maintained by the proactive Agent when later evidence changes that standing.
	// It is not the same as ExecutionRun.Summary ("what this run did"): a Task spans
	// several runs, and this field answers "how far has this thing got".
	Summary *string `gorm:"column:summary"`
	// LastProgressAt moves only when Summary actually changes, so a Task that keeps
	// resuming into waiting does not look alive. This is what makes stalled work
	// findable in one query, which UpdatedAt cannot do (any column write bumps it).
	LastProgressAt *time.Time `gorm:"column:last_progress_at;index:idx_task_last_progress"`
	// ExecutionSupplements are M5-only human clarifications/instructions, append-only
	// and isolated from Todo.context_snapshot.supplements.
	ExecutionSupplements datatypes.JSON `gorm:"column:execution_supplements"`
	ProjectID            *uint64        `gorm:"column:project_id;index:idx_task_project"`
	// RepoPath is an explicitly selected execution working copy. When absent,
	// M5 inherits the Jarvis server working directory and locates repos itself.
	RepoPath  *string   `gorm:"column:repo_path"`
	Version   int32     `gorm:"column:version;not null;default:0"`
	CreatedAt time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`

	Todo    *Todo    `gorm:"foreignKey:TodoID;constraint:OnDelete:RESTRICT"`
	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (Task) TableName() string { return "task" }

// Resource is a first-class reference to content carried by a message.
type Resource struct {
	ID              uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	ResourceType    string    `gorm:"column:resource_type;not null;index:idx_resource_type"`
	FileKey         *string   `gorm:"column:file_key;uniqueIndex:uk_resource_msg_key,priority:2"`
	MinuteToken     *string   `gorm:"column:minute_token"`
	DocToken        *string   `gorm:"column:doc_token"`
	URL             *string   `gorm:"column:url"`
	Name            *string   `gorm:"column:name"`
	MIMEType        *string   `gorm:"column:mime_type"`
	SizeBytes       *int64    `gorm:"column:size_bytes"`
	SourceMessageID *string   `gorm:"column:source_message_id;uniqueIndex:uk_resource_msg_key,priority:1;index:idx_resource_msg"`
	GroupID         *uint64   `gorm:"column:group_id;index:idx_resource_group"`
	LocalPath       *string   `gorm:"column:local_path"`
	Downloaded      bool      `gorm:"column:downloaded;not null;default:0"`
	ContentHash     *string   `gorm:"column:content_hash;index:idx_resource_content"`
	ExtractedText   *string   `gorm:"column:extracted_text"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt       time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (Resource) TableName() string { return "resource" }

// ScanRecord is append-only operational history for one capture attempt.
type ScanRecord struct {
	ID              uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	ScanType        string     `gorm:"column:scan_type;not null;index:idx_scan_type_time,priority:1"`
	GroupID         *uint64    `gorm:"column:group_id;index:idx_scan_group_time,priority:1"`
	ChatID          *string    `gorm:"column:chat_id"`
	WindowStart     *int64     `gorm:"column:window_start"`
	WindowEnd       *int64     `gorm:"column:window_end"`
	FetchedCount    int32      `gorm:"column:fetched_count;not null;default:0"`
	InsertedCount   int32      `gorm:"column:inserted_count;not null;default:0"`
	PageCount       int32      `gorm:"column:page_count;not null;default:0"`
	Status          string     `gorm:"column:status;not null;index:idx_scan_status"`
	ErrorType       *string    `gorm:"column:error_type"`
	ErrorMessage    *string    `gorm:"column:error_message"`
	HighWaterBefore *int64     `gorm:"column:high_water_before"`
	HighWaterAfter  *int64     `gorm:"column:high_water_after"`
	StartedAt       time.Time  `gorm:"column:started_at;not null;index:idx_scan_group_time,priority:2;index:idx_scan_type_time,priority:2"`
	FinishedAt      *time.Time `gorm:"column:finished_at"`
	DurationMS      *int32     `gorm:"column:duration_ms"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
}

func (ScanRecord) TableName() string { return "scan_record" }

// PrincipalProfile is the single decision-maker ("me") whose action clues M3
// extracts. It is a single-row table keyed by the owner's Feishu open_id; the
// background/preferences here are fed into the extraction prompt so the model
// knows who the principal is, what they own, and who their leader is. Kept
// separate from Person because its semantics (self-profile, preferences, direct
// leader) differ from a chat participant.
type PrincipalProfile struct {
	ID           uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	OpenID       string    `gorm:"column:open_id;not null;uniqueIndex:uk_principal_open_id"`
	Name         string    `gorm:"column:name;not null"`
	Department   *string   `gorm:"column:department"`
	Title        *string   `gorm:"column:title"`
	Background   *string   `gorm:"column:background"`  // 我是谁、负责什么方向
	Preferences  *string   `gorm:"column:preferences"` // 喜好、工作/沟通偏好
	LeaderOpenID *string   `gorm:"column:leader_open_id"`
	LeaderName   *string   `gorm:"column:leader_name"`
	CreatedAt    time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (PrincipalProfile) TableName() string { return "principal_profile" }

// ManagedResource is a manually maintained reference (doc/link/repo/note) that
// the owner curates from the admin UI. Unlike Resource (which capture derives
// automatically from messages), this table is human-owned and can be optionally
// linked to a Person, a Project, and/or the principal ("me") so the extraction
// tools can surface the right background material on demand.
type ManagedResource struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	Title         string    `gorm:"column:title;not null"`
	ResourceType  string    `gorm:"column:resource_type;not null;default:link;index:idx_managed_resource_type"`
	URL           *string   `gorm:"column:url"`
	Description   *string   `gorm:"column:description"`
	PersonID      *uint64   `gorm:"column:person_id;index:idx_managed_resource_person"`
	ProjectID     *uint64   `gorm:"column:project_id;index:idx_managed_resource_project"`
	LinkPrincipal bool      `gorm:"column:link_principal;not null;default:0;index:idx_managed_resource_principal"`
	IsActive      bool      `gorm:"column:is_active;not null;default:1;index:idx_managed_resource_active"`
	LastActiveAt  time.Time `gorm:"column:last_active_at;not null;default:'1970-01-01 00:00:00';index:idx_managed_resource_last_active"`
	CreatedAt     time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt     time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`

	Person  *Person  `gorm:"foreignKey:PersonID;constraint:OnDelete:SET NULL"`
	Project *Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:SET NULL"`
}

func (ManagedResource) TableName() string { return "managed_resource" }

// DailyDigest 是「每日进度总结」的落库缓存：一天一个 scope 一行，重算 upsert 覆盖
// （不留历史版本）。scope=person 时 scope_id 是 principal open_id；scope=group 时
// scope_id 是 feishu_group.id 的字符串。digest_date 是自然日（本地时区 00:00）。
// 生成是异步的，status 走 pending→generating→done/failed 状态机。
type DailyDigest struct {
	ID             uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	Scope          string         `gorm:"column:scope;not null;uniqueIndex:uk_scope_date,priority:1"`       // person / group
	ScopeID        string         `gorm:"column:scope_id;not null;uniqueIndex:uk_scope_date,priority:2"`    // person=principal open_id；group=feishu_group.id 字符串
	DigestDate     datatypes.Date `gorm:"column:digest_date;not null;uniqueIndex:uk_scope_date,priority:3"` // 自然日（本地时区）
	Summary        string         `gorm:"column:summary"`                                                   // 生成的一段中文进度总结
	Status         string         `gorm:"column:status;not null;default:pending"`                           // pending / generating / done / failed
	TriggerType    string         `gorm:"column:trigger_type;not null;default:manual"`                      // manual / schedule
	SourceCount    int            `gorm:"column:source_count;not null;default:0"`                           // 所有成功纳入的证据条数
	SourceCoverage datatypes.JSON `gorm:"column:source_coverage"`                                           // 各数据源 status/count/note
	Engine         string         `gorm:"column:engine;not null"`                                           // codex
	ErrorDetail    *string        `gorm:"column:error_detail"`                                              // 失败原因（fail 时）
	StartedAt      *time.Time     `gorm:"column:started_at"`                                                // 本轮开始生成时刻
	CutoffAt       *time.Time     `gorm:"column:cutoff_at"`                                                 // 本轮证据截止时刻
	GeneratedAt    *time.Time     `gorm:"column:generated_at"`                                              // 生成完成时刻
	CreatedAt      time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt      time.Time      `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (DailyDigest) TableName() string { return "daily_digest" }

// ScheduledTask is a durable time trigger. Each occurrence materializes a Task
// for M5; it never executes the instruction itself. ContextSnapshot freezes the
// background available when the schedule was created.
type ScheduledTask struct {
	ID              uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	DispatchKind    string         `gorm:"column:dispatch_kind;not null;default:create_task;index:idx_scheduled_task_dispatch"`
	SubjectType     *string        `gorm:"column:subject_type"`
	SubjectID       *uint64        `gorm:"column:subject_id;index:idx_scheduled_task_subject"`
	SourceRunID     *uint64        `gorm:"column:source_run_id;uniqueIndex:uk_scheduled_task_source_run"`
	DispatchPayload datatypes.JSON `gorm:"column:dispatch_payload"`
	Title           string         `gorm:"column:title;not null"`
	ActionType      string         `gorm:"column:action_type;not null;default:agent_task"`
	Instruction     string         `gorm:"column:instruction;not null"`
	ContextSnapshot datatypes.JSON `gorm:"column:context_snapshot;not null"`
	ScheduleType    string         `gorm:"column:schedule_type;not null"` // once / daily / interval
	DailyTime       *string        `gorm:"column:daily_time"`             // HH:mm in server local timezone
	IntervalMinutes *int           `gorm:"column:interval_minutes"`
	RunAt           *time.Time     `gorm:"column:run_at"`
	NextRunAt       time.Time      `gorm:"column:next_run_at;not null;index:idx_scheduled_task_due,priority:3"`
	Enabled         bool           `gorm:"column:enabled;not null;index:idx_scheduled_task_due,priority:1"`
	Status          string         `gorm:"column:status;not null;default:active;index:idx_scheduled_task_due,priority:2"` // binding / active / running / completed
	LastRunStatus   *string        `gorm:"column:last_run_status"`                                                        // done / failed
	LastTaskID      *uint64        `gorm:"column:last_task_id;index:idx_scheduled_task_last_task"`
	LastResult      *string        `gorm:"column:last_result"`
	LastErrorDetail *string        `gorm:"column:last_error_detail"`
	LastStartedAt   *time.Time     `gorm:"column:last_started_at"`
	LastFinishedAt  *time.Time     `gorm:"column:last_finished_at"`
	CreatedAt       time.Time      `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt       time.Time      `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (ScheduledTask) TableName() string { return "scheduled_task" }

// CoreModels returns the canonical dependency-ordered migration list.
func CoreModels() []any {
	return []any{
		&Project{},
		&KeyMatter{},
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
