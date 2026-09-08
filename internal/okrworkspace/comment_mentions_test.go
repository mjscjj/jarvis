package okrworkspace

import (
	"context"
	"strings"
	"testing"
)

type commentBroadcastSenderStub struct {
	openID         string
	name           string
	authorEmail    string
	text           string
	idempotencyKey string
}

func (stub *commentBroadcastSenderStub) SendTextToMainAppUser(_ context.Context, openID, name, authorEmail, text, idempotencyKey string) error {
	stub.openID, stub.name, stub.authorEmail, stub.text, stub.idempotencyKey = openID, name, authorEmail, text, idempotencyKey
	return nil
}

func TestBotCommentMentionNotificationContainsRequiredContextAndDeepLink(t *testing.T) {
	sender := &commentBroadcastSenderStub{}
	notifier, err := NewBotCommentMentionNotifier(sender, "https://emily.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	input := CommentMentionNotification{
		Recipient: CommentMention{OpenID: "ou_bob", Name: "Bob"}, CommentID: "comment-1", AuthorName: "Alice", AuthorEmail: "alice@example.com",
		Quarter: "2026-Q3", Week: "2026-W35", ObjectiveTitle: "O 原文", KRTitle: "KR 原文",
		OriginalText: "对应字段原文", Content: "@Bob 请确认原文", Tab: "review-meeting",
	}
	if err := notifier.NotifyCommentMention(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Alice 在 Jarvis OKR 评论中 @ 了你", "页面：Review 会议", "周次：2026-W35", "O：O 原文", "KR：KR 原文", "对应原文：\n对应字段原文", "评论：\n@Bob 请确认原文", "https://emily.example.com/#/weekly-report?"} {
		if !strings.Contains(sender.text, expected) {
			t.Fatalf("message %q does not contain %q", sender.text, expected)
		}
	}
	if !strings.Contains(sender.text, "comment_id=comment-1") || !strings.Contains(sender.text, "tab=review-meeting") || !strings.Contains(sender.text, "week=2026-W35") || len(sender.idempotencyKey) > 50 || !strings.HasPrefix(sender.idempotencyKey, "okr-cmt-") {
		t.Fatalf("message = %q, idempotency key = %q", sender.text, sender.idempotencyKey)
	}
	if sender.openID != "ou_bob" || sender.name != "Bob" || sender.authorEmail != "alice@example.com" {
		t.Fatalf("recipient forwarding = %#v", sender)
	}
}

func TestBotCommentMentionNotificationBuildsPlanDeepLink(t *testing.T) {
	sender := &commentBroadcastSenderStub{}
	notifier, err := NewBotCommentMentionNotifier(sender, "https://emily.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	input := CommentMentionNotification{
		Recipient: CommentMention{OpenID: "ou_owner", Name: "负责人"},
		CommentID: "comment-plan", AuthorName: "张若怡", Quarter: "2026-Q4", PlanID: "plan-1",
		PlanTitle: "2026 Q4 Biz OKR Plan", ObjectiveTitle: "O 原文", KRTitle: "KR 原文",
		OriginalText: "指标原文", Content: "@负责人 请确认", Tab: commentSourceTabOKRPlan,
	}
	if err := notifier.NotifyCommentMention(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"张若怡 在 Jarvis OKR 评论中 @ 了你", "页面：Biz OKR Plan", "Plan：2026 Q4 Biz OKR Plan",
		"quarter=2026-Q4", "plan_id=plan-1", "tab=okr-plan", "comment_id=comment-plan",
	} {
		if !strings.Contains(sender.text, expected) {
			t.Fatalf("message %q does not contain %q", sender.text, expected)
		}
	}
}
