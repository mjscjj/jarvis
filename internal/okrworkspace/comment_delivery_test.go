package okrworkspace

import (
	"fmt"
	"jarvis/internal/okrworkspace/domain"
	"testing"
	"time"
)

func TestCommentDeliveryRetryVerifiesReceiptWithoutResending(t *testing.T) {
	db := openWorkspaceTestDB(t)
	svc, _ := NewService(db)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	stub := &commentMentionNotifierStub{err: fmt.Errorf("readback failed")}
	svc.SetCommentMentionNotifier(stub)
	view, err := svc.CreateComment(t.Context(), CreateCommentInput{Quarter: "2026-Q3", Week: "2026-W35", Content: "@Bob 检查", Mentions: []CommentMention{{Email: "bob@example.test", Name: "Bob"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.items) != 0 || len(view.Notifications) != 1 || view.Notifications[0].Status != "pending" {
		t.Fatalf("HTTP save waited for notification: %+v", view)
	}
	if err := svc.processPendingCommentDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	view.Notifications, err = svc.commentDeliveries(t.Context(), view.ID)
	if len(view.Notifications) != 1 || view.Notifications[0].Status != "unknown" || view.Notifications[0].MessageID == "" {
		t.Fatalf("receipt=%+v", view.Notifications)
	}
	stub.err = nil
	got, err := svc.RetryCommentNotifications(t.Context(), view.ID, "bob@example.test", false)
	if err != nil || got[0].Status != "delivered" {
		t.Fatalf("retry=%+v %v", got, err)
	}
	_, err = svc.RetryCommentNotifications(t.Context(), view.ID, "", false)
	if err != nil || len(stub.items) != 1 {
		t.Fatalf("resent delivered comment: calls=%d err=%v", len(stub.items), err)
	}
	var row domain.CommentDelivery
	if err = db.First(&row).Error; err != nil || row.Attempts != 1 {
		t.Fatalf("row=%+v err=%v", row, err)
	}
}

func TestStaleSendingDeliveryRequiresExplicitResend(t *testing.T) {
	db := openWorkspaceTestDB(t)
	svc, _ := NewService(db)
	stub := &commentMentionNotifierStub{}
	svc.SetCommentMentionNotifier(stub)
	record := domain.CommentDelivery{
		CommentID: "comment-stale", Email: "bob@example.test", Name: "Bob", AppID: stub.AppID(),
		Payload: `{"recipient":{"email":"bob@example.test","name":"Bob"}}`, Status: "sending",
		UpdatedAt: time.Now().Add(-commentDeliveryClaimTimeout - time.Second),
	}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	view, err := svc.commentDeliveries(t.Context(), record.CommentID)
	if err != nil || len(view) != 1 || view[0].Status != "unknown" {
		t.Fatalf("recovered delivery = %+v, %v", view, err)
	}
	if _, err := svc.deliverComment(t.Context(), record.CommentID, record.Email, false); err != nil {
		t.Fatal(err)
	}
	if len(stub.items) != 0 {
		t.Fatal("unknown delivery resent without confirmation")
	}
	view, err = svc.deliverComment(t.Context(), record.CommentID, record.Email, true)
	if err != nil || len(stub.items) != 1 || view[0].Status != "delivered" {
		t.Fatalf("explicit resend = %+v calls=%d err=%v", view, len(stub.items), err)
	}
}

func TestOwnerKeySurvivesDisplayNameChange(t *testing.T) {
	a := OwnerView{Email: "alice@EXAMPLE.TEST", Name: "Alice"}
	b := OwnerView{Email: "alice@example.test", Name: "艾丽丝"}
	if ownerKey(a) != ownerKey(b) || ownerPersonID(a) != ownerPersonID(b) {
		t.Fatal("display name changed identity")
	}
	if ownerKey(a) == ownerKey(OwnerView{Name: "Alice"}) {
		t.Fatal("unresolved owner merged with resolved identity")
	}
}
