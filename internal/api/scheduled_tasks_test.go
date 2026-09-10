package api

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/scheduledtask"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWeeklyScheduledTaskAPI(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "schedules.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ScheduledTask{}); err != nil {
		t.Fatal(err)
	}
	service, err := scheduledtask.NewCRUDService(db)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.POST("/api/scheduled-tasks", CreateScheduledTask(service))
	h.PUT("/api/scheduled-tasks/:scheduled_task_id", UpdateScheduledTask(service))
	h.GET("/api/scheduled-tasks", ListScheduledTasks(service))
	h.GET("/api/scheduled-tasks/:scheduled_task_id", GetScheduledTask(service))
	for _, weekday := range []string{"null", "-1", "7", "1.5", `"1"`} {
		body := fmt.Sprintf(`{"title":"weekly","instruction":"run","schedule_type":"weekly","daily_time":"09:00","weekday":%s}`, weekday)
		r := ut.PerformRequest(h.Engine, "POST", "/api/scheduled-tasks", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
		if r.StatusCode() != 400 {
			t.Fatalf("weekday %s: HTTP %d %s", weekday, r.StatusCode(), r.Body())
		}
	}
	for _, tc := range []struct{ method, path, body, want string }{
		{"POST", "/api/scheduled-tasks", `{"title":"weekly","instruction":"run","schedule_type":"weekly","daily_time":"09:00","weekday":0}`, `"weekday":0`},
		{"GET", "/api/scheduled-tasks/1", "", `"weekday":0`},
		{"GET", "/api/scheduled-tasks", "", `"weekday":0`},
		{"PUT", "/api/scheduled-tasks/1", `{"title":"weekly","instruction":"run","schedule_type":"weekly","daily_time":"18:00","weekday":5}`, `"weekday":5`},
		{"PUT", "/api/scheduled-tasks/1", `{"title":"daily","instruction":"run","schedule_type":"daily","daily_time":"18:00","weekday":5}`, `"weekday":null`},
	} {
		r := ut.PerformRequest(h.Engine, tc.method, tc.path, &ut.Body{Body: strings.NewReader(tc.body), Len: len(tc.body)}).Result()
		if r.StatusCode() != 200 || !strings.Contains(string(r.Body()), tc.want) {
			t.Fatalf("%s %s: HTTP %d %s", tc.method, tc.path, r.StatusCode(), r.Body())
		}
	}
}

func TestScheduledTaskListOmitsDetailBodies(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "schedules.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ScheduledTask{}); err != nil {
		t.Fatal(err)
	}
	contextSnapshot := `{"large_context":"` + strings.Repeat("context", 1000) + `"}`
	dispatchPayload := `{"large_payload":"` + strings.Repeat("payload", 1000) + `"}`
	row := domain.ScheduledTask{
		DispatchKind: "create_task", DispatchPayload: []byte(dispatchPayload),
		Title: "compact list", ActionType: "agent_task", Instruction: "run",
		ContextSnapshot: []byte(contextSnapshot), ScheduleType: "once",
		NextRunAt: time.Now().Add(time.Hour), Enabled: true, Status: "active",
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	service, err := scheduledtask.NewCRUDService(db)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	h.GET("/api/scheduled-tasks", ListScheduledTasks(service))
	h.GET("/api/scheduled-tasks/:scheduled_task_id", GetScheduledTask(service))

	list := ut.PerformRequest(h.Engine, "GET", "/api/scheduled-tasks", nil).Result()
	if list.StatusCode() != 200 {
		t.Fatalf("list HTTP %d: %s", list.StatusCode(), list.Body())
	}
	var listBody map[string]any
	if err := json.Unmarshal(list.Body(), &listBody); err != nil {
		t.Fatal(err)
	}
	listJSON := string(list.Body())
	for _, forbidden := range []string{"context_snapshot", "dispatch_payload", "large_context", "large_payload"} {
		if strings.Contains(listJSON, forbidden) {
			t.Fatalf("scheduled task list leaked %q: %s", forbidden, listJSON)
		}
	}

	detail := ut.PerformRequest(h.Engine, "GET", fmt.Sprintf("/api/scheduled-tasks/%d", row.ID), nil).Result()
	if detail.StatusCode() != 200 {
		t.Fatalf("detail HTTP %d: %s", detail.StatusCode(), detail.Body())
	}
	for _, want := range []string{"context_snapshot", "dispatch_payload", "large_context", "large_payload"} {
		if !strings.Contains(string(detail.Body()), want) {
			t.Fatalf("scheduled task detail lost %q: %s", want, detail.Body())
		}
	}
}
