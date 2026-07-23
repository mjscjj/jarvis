package dailydigest

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

func TestCatchUpDue(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	schedule, err := cron.ParseStandard("0 19 * * *")
	if err != nil {
		t.Fatalf("parse schedule: %v", err)
	}
	tests := []struct {
		name string
		now  string
		want bool
	}{
		{name: "before schedule", now: "2026-07-23 18:59:59", want: false},
		{name: "at schedule", now: "2026-07-23 19:00:00", want: true},
		{name: "after schedule", now: "2026-07-23 23:00:00", want: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			now, err := time.ParseInLocation("2006-01-02 15:04:05", test.now, location)
			if err != nil {
				t.Fatalf("parse now: %v", err)
			}
			if got := catchUpDue(schedule, now, location); got != test.want {
				t.Fatalf("catchUpDue(%s) = %v, want %v", test.now, got, test.want)
			}
		})
	}
}
