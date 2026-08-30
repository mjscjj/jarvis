package domain

import "time"

// PluginInstallation stores only the machine-owned lifecycle of one installed
// integration. Plugin metadata and collection semantics stay in the registry
// and its Skill; collected evidence continues through the generic clue stream.
type PluginInstallation struct {
	PluginID        string    `gorm:"column:plugin_id;primaryKey"`
	Enabled         bool      `gorm:"column:enabled;not null;default:false"`
	Revision        uint64    `gorm:"column:revision;not null;default:0"`
	ScheduledTaskID *uint64   `gorm:"column:scheduled_task_id;uniqueIndex:uk_plugin_schedule"`
	CreatedAt       time.Time `gorm:"column:created_at;not null;default:CURRENT_TIMESTAMP;autoCreateTime"`
	UpdatedAt       time.Time `gorm:"column:updated_at;not null;default:CURRENT_TIMESTAMP;autoUpdateTime"`
}

func (PluginInstallation) TableName() string { return "plugin_installation" }

// PluginModels returns the plugin-owned schema. It is kept separate from core
// task and event models so the integration can be removed without changing
// their contracts.
func PluginModels() []any {
	return []any{&PluginInstallation{}}
}
