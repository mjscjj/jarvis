package scheduledtask

import (
	"strings"
	"testing"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSkillBindingsPreservesScheduleState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ScheduledTask{}); err != nil {
		t.Fatal(err)
	}
	next := time.Date(2026, 8, 31, 1, 0, 0, 0, time.UTC)
	record := domain.ScheduledTask{
		Title: "只读巡检", Instruction: "读取 okr-progress-sync Skill", ActionType: "agent_task",
		ContextSnapshot: datatypes.JSON(`{"mode":"read_only","module":"okr","skill":"okr-progress-sync"}`),
		ScheduleType:    "interval", IntervalMinutes: intPointer(360), NextRunAt: next, Enabled: false, Status: "active",
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	count, err := MigrateSkillBindings(t.Context(), db, []SkillBindingMigration{{
		FromSkill: "okr-progress-sync", ToSkill: "weekly-report-progress-sync", FromModule: "okr", ToModule: "agency-okr",
	}})
	if err != nil || count != 1 {
		t.Fatalf("MigrateSkillBindings() = %d, %v", count, err)
	}
	var result domain.ScheduledTask
	if err := db.First(&result, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if result.Enabled || result.Status != "active" || !result.NextRunAt.Equal(next) || !strings.Contains(result.Instruction, "weekly-report-progress-sync") || string(result.ContextSnapshot) != `{"mode":"read_only","module":"agency-okr","skill":"weekly-report-progress-sync"}` {
		t.Fatalf("migrated schedule = %+v context=%s", result, result.ContextSnapshot)
	}
}

func intPointer(value int) *int { return &value }
