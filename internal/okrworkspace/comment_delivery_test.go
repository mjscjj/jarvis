package okrworkspace

import (
	"fmt"
	"jarvis/internal/okrworkspace/domain"
	"testing"
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
	if len(view.Notifications) != 1 || view.Notifications[0].Status != "unknown" || view.Notifications[0].MessageID == "" {
		t.Fatalf("receipt=%+v", view.Notifications)
	}
	stub.err = nil
	got, err := svc.RetryCommentNotifications(t.Context(), view.ID, "bob@example.test")
	if err != nil || got[0].Status != "delivered" {
		t.Fatalf("retry=%+v %v", got, err)
	}
	_, err = svc.RetryCommentNotifications(t.Context(), view.ID, "")
	if err != nil || len(stub.items) != 1 {
		t.Fatalf("resent delivered comment: calls=%d err=%v", len(stub.items), err)
	}
	var row domain.CommentDelivery
	if err = db.First(&row).Error; err != nil || row.Attempts != 1 {
		t.Fatalf("row=%+v err=%v", row, err)
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
