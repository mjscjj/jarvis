package domain

import (
	"time"
)

// RelationFact links two existing domain entities. Identity stays structured;
// Description explains the relationship in natural language for humans and
// models. The service canonicalizes the pair so A-B and B-A share one row.
type RelationFact struct {
	ID uint64 `gorm:"column:id;primaryKey;autoIncrement"`

	EntityAType string `gorm:"column:entity_a_type;not null;uniqueIndex:uk_relation_fact_pair,priority:1;index:idx_relation_fact_entity_a,priority:1"`
	EntityAID   uint64 `gorm:"column:entity_a_id;not null;uniqueIndex:uk_relation_fact_pair,priority:2;index:idx_relation_fact_entity_a,priority:2"`
	EntityBType string `gorm:"column:entity_b_type;not null;uniqueIndex:uk_relation_fact_pair,priority:3;index:idx_relation_fact_entity_b,priority:1"`
	EntityBID   uint64 `gorm:"column:entity_b_id;not null;uniqueIndex:uk_relation_fact_pair,priority:4;index:idx_relation_fact_entity_b,priority:2"`
	Description string `gorm:"column:description;not null"`

	// ValidFrom and ValidUntil bound the period this relationship holds. Both are
	// optional and independent: a nil ValidFrom means the start is unknown, a nil
	// ValidUntil means the relationship still holds. A set ValidUntil is how a
	// past relationship ("used to own this project") stays queryable instead of
	// being deleted or rewritten in Description.
	ValidFrom  *time.Time `gorm:"column:valid_from"`
	ValidUntil *time.Time `gorm:"column:valid_until"`

	CreatedAt time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (RelationFact) TableName() string { return "relation_fact" }

func KnowledgeModels() []any { return []any{&RelationFact{}} }
