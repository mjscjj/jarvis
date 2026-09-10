package scheduledtask

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type resumeFunc func(context.Context, uint64, uint64, string) error

func (f resumeFunc) ResumeTask(ctx context.Context, taskID, runID uint64, reason string) error {
	return f(ctx, taskID, runID, reason)
}

func TestResumeDispatchHandlesTerminalTaskAndCloseRace(t *testing.T) {
	for _, scenario := range []string{"already closed", "close wins race", "resume succeeds", "real failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&domain.Task{}, &domain.ScheduledTask{}); err != nil {
				t.Fatal(err)
			}
			task := domain.Task{Title: "waiting", ActionType: "investigate", SourceType: "manual", SourcePayload: datatypes.JSON(`{}`), Status: "waiting"}
			if scenario == "already closed" {
				task.Status = "done"
			}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			subjectType, runID := "task", uint64(7)
			row := domain.ScheduledTask{SubjectType: &subjectType, SubjectID: &task.ID, SourceRunID: &runID,
				DispatchKind: "resume_task", DispatchPayload: datatypes.JSON(`{"reason":"check material"}`),
				Title: "resume", ActionType: "investigate", Instruction: "resume", ContextSnapshot: datatypes.JSON(`{}`),
				ScheduleType: "once", Status: "running", NextRunAt: time.Now()}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			calls := 0
			service := &Service{db: db, now: time.Now, resumer: resumeFunc(func(ctx context.Context, taskID, sourceRunID uint64, reason string) error {
				calls++
				if taskID != task.ID || sourceRunID != runID || reason != "check material" {
					t.Fatalf("wrong resume arguments: %d %d %s", taskID, sourceRunID, reason)
				}
				if scenario == "close wins race" {
					if err := db.Model(&domain.Task{}).Where("id = ?", task.ID).Update("status", "done").Error; err != nil {
						t.Fatal(err)
					}
				}
				if scenario != "resume succeeds" {
					return errors.New("resume claim rejected")
				}
				return nil
			})}
			err = service.dispatchResume(t.Context(), &row)
			if (err != nil) != (scenario == "real failure") {
				t.Fatalf("dispatch error = %v", err)
			}
			if scenario == "already closed" && calls != 0 || scenario != "already closed" && calls != 1 {
				t.Fatalf("resume calls = %d", calls)
			}
			var stored domain.ScheduledTask
			if err := db.First(&stored, row.ID).Error; err != nil {
				t.Fatal(err)
			}
			want := "done"
			if scenario == "real failure" {
				want = "failed"
			}
			if stored.Status != "completed" || stored.LastRunStatus == nil || *stored.LastRunStatus != want {
				t.Fatalf("stored schedule = %+v", stored)
			}
			if strings.Contains(scenario, "closed") || scenario == "close wins race" {
				if stored.LastResult == nil || !strings.Contains(*stored.LastResult, "未启动执行") {
					t.Fatalf("missing obsolete trigger receipt: %+v", stored)
				}
			}
		})
	}
}
