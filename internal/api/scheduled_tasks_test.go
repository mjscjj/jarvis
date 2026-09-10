package api

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

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
	for _, weekday := range []string{"null", "-1", "0", "8", "1.5", `"1"`} {
		body := fmt.Sprintf(`{"title":"weekly","instruction":"run","schedule_type":"weekly","daily_time":"09:00","weekday":%s}`, weekday)
		r := ut.PerformRequest(h.Engine, "POST", "/api/scheduled-tasks", &ut.Body{Body: strings.NewReader(body), Len: len(body)}).Result()
		if r.StatusCode() != 400 {
			t.Fatalf("weekday %s: HTTP %d %s", weekday, r.StatusCode(), r.Body())
		}
	}
	for _, tc := range []struct{ method, path, body, want string }{
		{"POST", "/api/scheduled-tasks", `{"title":"weekly","instruction":"run","schedule_type":"weekly","daily_time":"09:00","weekday":7}`, `"weekday":7`},
		{"GET", "/api/scheduled-tasks/1", "", `"weekday":7`},
		{"GET", "/api/scheduled-tasks", "", `"weekday":7`},
		{"PUT", "/api/scheduled-tasks/1", `{"title":"weekly","instruction":"run","schedule_type":"weekly","daily_time":"18:00","weekday":5}`, `"weekday":5`},
		{"PUT", "/api/scheduled-tasks/1", `{"title":"daily","instruction":"run","schedule_type":"daily","daily_time":"18:00","weekday":5}`, `"weekday":null`},
	} {
		r := ut.PerformRequest(h.Engine, tc.method, tc.path, &ut.Body{Body: strings.NewReader(tc.body), Len: len(tc.body)}).Result()
		if r.StatusCode() != 200 || !strings.Contains(string(r.Body()), tc.want) {
			t.Fatalf("%s %s: HTTP %d %s", tc.method, tc.path, r.StatusCode(), r.Body())
		}
	}
}
