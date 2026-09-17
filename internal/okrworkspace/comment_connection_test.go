package okrworkspace

import (
	"context"
	"testing"
	"time"

	"jarvis/internal/okrworkspace/domain"
)

// Production serializes SQLite work on one connection. Notification context
// must use the transaction that inserted the comment, including weekly metrics.
func TestCommentNotificationUsesSingleConnection(t *testing.T) {
	for _, kind := range []string{"plan", "weekly-metric"} {
		t.Run(kind, func(t *testing.T) {
			db := openWorkspaceTestDB(t)
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			sqlDB.SetMaxOpenConns(1)
			objective := domain.Objective{ID: "o", Quarter: "2026-Q4", Title: "Objective"}
			kr := domain.KR{ID: "kr", ObjectiveID: objective.ID, Title: "KR"}
			input := CreateCommentInput{Quarter: objective.Quarter, TargetType: "kr", TargetID: kr.ID, Content: "Please confirm"}
			rows := []any{&objective, &kr, &domain.KROwner{KRID: kr.ID, Email: "owner@example.test", Name: "Owner"}}
			if kind == "plan" {
				objective.PlanID, input.PlanID = "plan", "plan"
				rows = append(rows, &domain.OKRPlan{ID: "plan", Quarter: objective.Quarter, Title: "Plan"})
			} else {
				input.Week, input.TargetType, input.TargetID = "2026-W41", "metric", "metric"
				rows = append(rows, &domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: input.Week, OpenedBy: "test"},
					&domain.WeeklyKRCore{KRID: kr.ID, Week: input.Week, Metrics: []domain.WeeklyMetric{{ID: "metric", Text: "Weekly target"}}})
			}
			for _, row := range rows {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			service, err := NewService(db)
			if err != nil {
				t.Fatal(err)
			}
			stub := &commentMentionNotifierStub{}
			if err := service.SetCommentMentionNotifier(stub); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			created, err := service.CreateComment(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if len(created.Notifications) != 1 {
				t.Fatalf("notification ledger = %#v", created.Notifications)
			}
			if err := service.processPendingCommentDeliveries(ctx); err != nil {
				t.Fatal(err)
			}
			if len(stub.items) != 1 || stub.items[0].KRID != kr.ID {
				t.Fatalf("notification context = %#v", stub.items)
			}
			if kind == "weekly-metric" && stub.items[0].OriginalText != "Weekly target" {
				t.Fatalf("weekly context = %#v", stub.items[0])
			}
			var count int64
			if err := db.WithContext(ctx).Model(&domain.PageComment{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("read after comment: count=%d err=%v", count, err)
			}
		})
	}
}
