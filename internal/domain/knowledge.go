package domain

import (
	"time"

	"gorm.io/datatypes"
)

// RelationFact stores a time-bounded, sourced relationship that is not already
// represented by a typed foreign key. SubjectType/SubjectID and
// ObjectType/ObjectID deliberately point at existing domain tables without a
// generic entity registry; the knowledge service validates those polymorphic
// references before every write.
type RelationFact struct {
	ID uint64 `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`

	SubjectType string `gorm:"column:subject_type;type:varchar(32);not null;index:idx_relation_fact_subject,priority:1"`
	SubjectID   uint64 `gorm:"column:subject_id;type:bigint unsigned;not null;index:idx_relation_fact_subject,priority:2"`
	Predicate   string `gorm:"column:predicate;type:varchar(64);not null;index:idx_relation_fact_subject,priority:3"`

	ObjectType *string        `gorm:"column:object_type;type:varchar(32);index:idx_relation_fact_object,priority:1"`
	ObjectID   *uint64        `gorm:"column:object_id;type:bigint unsigned;index:idx_relation_fact_object,priority:2"`
	ValueJSON  datatypes.JSON `gorm:"column:value_json;type:json;check:ck_relation_fact_target,((object_type IS NOT NULL AND object_id IS NOT NULL AND value_json IS NULL) OR (object_type IS NULL AND object_id IS NULL AND value_json IS NOT NULL))"`

	AssertionKind string   `gorm:"column:assertion_kind;type:varchar(16);not null"`
	Confidence    *float64 `gorm:"column:confidence;type:decimal(4,3);check:ck_relation_fact_confidence,confidence IS NULL OR (confidence >= 0 AND confidence <= 1)"`

	ValidFrom *time.Time `gorm:"column:valid_from;type:datetime;index:idx_relation_fact_validity,priority:1"`
	ValidTo   *time.Time `gorm:"column:valid_to;type:datetime;index:idx_relation_fact_validity,priority:2"`

	Status           string     `gorm:"column:status;type:varchar(16);not null;default:active;index:idx_relation_fact_status"`
	SupersededByID   *uint64    `gorm:"column:superseded_by_id;type:bigint unsigned;index:idx_relation_fact_superseded_by"`
	RetractedAt      *time.Time `gorm:"column:retracted_at;type:datetime"`
	RetractedBy      *string    `gorm:"column:retracted_by;type:varchar(64)"`
	RetractionReason *string    `gorm:"column:retraction_reason;type:varchar(512)"`

	SourceType  string  `gorm:"column:source_type;type:varchar(32);not null;index:idx_relation_fact_source,priority:1"`
	SourceID    string  `gorm:"column:source_id;type:varchar(128);not null;index:idx_relation_fact_source,priority:2"`
	SourceQuote *string `gorm:"column:source_quote;type:text"`

	Model         *string `gorm:"column:model;type:varchar(64)"`
	PromptVersion *string `gorm:"column:prompt_version;type:varchar(32)"`
	DedupKey      string  `gorm:"column:dedup_key;type:char(64);not null;uniqueIndex:uk_relation_fact_dedup"`

	CreatedAt time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (RelationFact) TableName() string { return "relation_fact" }

func KnowledgeModels() []any { return []any{&RelationFact{}} }
