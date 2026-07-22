package domain

import (
	"time"
)

// RelationFact links two existing domain entities. Identity stays structured;
// Description explains the relationship in natural language for humans and
// models. The service canonicalizes the pair so A-B and B-A share one row.
type RelationFact struct {
	ID uint64 `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement"`

	EntityAType string `gorm:"column:entity_a_type;type:varchar(32);not null;uniqueIndex:uk_relation_fact_pair,priority:1;index:idx_relation_fact_entity_a,priority:1"`
	EntityAID   uint64 `gorm:"column:entity_a_id;type:bigint unsigned;not null;uniqueIndex:uk_relation_fact_pair,priority:2;index:idx_relation_fact_entity_a,priority:2"`
	EntityBType string `gorm:"column:entity_b_type;type:varchar(32);not null;uniqueIndex:uk_relation_fact_pair,priority:3;index:idx_relation_fact_entity_b,priority:1"`
	EntityBID   uint64 `gorm:"column:entity_b_id;type:bigint unsigned;not null;uniqueIndex:uk_relation_fact_pair,priority:4;index:idx_relation_fact_entity_b,priority:2"`
	Description string `gorm:"column:description;type:text;not null"`

	CreatedAt time.Time `gorm:"column:created_at;type:timestamp;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (RelationFact) TableName() string { return "relation_fact" }

func KnowledgeModels() []any { return []any{&RelationFact{}} }
