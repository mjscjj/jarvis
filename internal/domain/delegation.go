package domain

import (
	"jarvis/internal/datatypes"
	"time"
)

// DelegationProgress is the mutable check result of an M3 Todo. The Todo is
// the identity and owns the original context; Task status never closes it.
// No row means the Todo has not been checked yet (version 0), not that it is absent.
type DelegationProgress struct {
	TodoID    uint64         `gorm:"column:todo_id;primaryKey;autoIncrement:false"`
	Content   datatypes.JSON `gorm:"column:content;not null"`
	ClosedAt  *time.Time     `gorm:"column:closed_at;index:idx_delegation_closed"`
	Version   int32          `gorm:"column:version;not null"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	Todo      *Todo          `gorm:"foreignKey:TodoID;constraint:OnDelete:RESTRICT"`
}

func (DelegationProgress) TableName() string { return "delegation_progress" }
