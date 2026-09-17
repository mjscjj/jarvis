package okrworkspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"jarvis/internal/okrworkspace/domain"
)

type commentBroadcastSenderStub struct {
	openID         string
	name           string
	authorEmail    string
	card           string
	idempotencyKey string
}

func (stub *commentBroadcastSenderStub) SendCardToEmail(_ context.Context, email, authorEmail, card, idempotencyKey string) (string, error) {
	stub.openID, stub.authorEmail, stub.card, stub.idempotencyKey = email, authorEmail, card, idempotencyKey
	return "om_test_notice", nil
}
func (stub *commentBroadcastSenderStub) VerifyMessage(context.Context, string) error { return nil }
func (stub *commentBroadcastSenderStub) AppID() string                               { return "test-app" }

func TestBotCommentMentionNotificationContainsRequiredContextAndDeepLink(t *testing.T) {
	sender := &commentBroadcastSenderStub{}
	notifier, err := NewBotCommentMentionNotifier(sender, "https://emily.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	input := CommentMentionNotification{
		Recipient: CommentMention{Email: "bob@example.test", Name: "Bob"}, CommentID: "comment-1", AuthorName: "Alice", AuthorEmail: "alice@example.com",
		Quarter: "2026-Q3", Week: "2026-W35", ObjectiveTitle: "O 原文", KRTitle: "KR 原文",
		OriginalText: "对应字段 *原文*", Content: "@Bob 请确认 [原文]", Tab: "review-meeting",
	}
	if _, err := notifier.NotifyCommentMention(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	var card struct {
		Schema string `json:"schema"`
		Config struct {
			WidthMode string `json:"width_mode"`
		} `json:"config"`
		Header struct {
			Subtitle struct {
				Content string `json:"content"`
			} `json:"subtitle"`
		} `json:"header"`
		Body struct {
			Elements []json.RawMessage `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(sender.card), &card); err != nil {
		t.Fatal(err)
	}
	if card.Schema != "2.0" || card.Config.WidthMode != "compact" || card.Header.Subtitle.Content != "Alice @ 了你" || len(card.Body.Elements) != 3 {
		t.Fatalf("card = %s", sender.card)
	}
	var bodyText struct {
		Tag  string `json:"tag"`
		Text struct {
			Content string `json:"content"`
			Lines   int    `json:"lines"`
		} `json:"text"`
	}
	if err := json.Unmarshal(card.Body.Elements[0], &bodyText); err != nil {
		t.Fatal(err)
	}
	if bodyText.Tag != "div" || bodyText.Text.Lines != 6 || bodyText.Text.Content != "**原文：**对应字段 &#42;原文&#42;\n**评论：**@Bob 请确认 &#91;原文&#93;" {
		t.Fatalf("body text = %#v", bodyText)
	}
	var contextPanel struct {
		Tag      string `json:"tag"`
		Expanded bool   `json:"expanded"`
		Elements []struct {
			Text struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(card.Body.Elements[1], &contextPanel); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"页面：Review 会议", "周期：2026-W35", "O：O 原文", "KR：KR 原文"} {
		if contextPanel.Tag != "collapsible_panel" || contextPanel.Expanded || len(contextPanel.Elements) != 1 || !strings.Contains(contextPanel.Elements[0].Text.Content, expected) {
			t.Fatalf("context panel = %#v, want %q", contextPanel, expected)
		}
	}
	if strings.Contains(contextPanel.Elements[0].Text.Content, "对应字段") {
		t.Fatalf("original text should be visible, not repeated in context panel: %#v", contextPanel)
	}
	if !strings.Contains(sender.card, "https://emily.example.com/#/weekly-report?") || !strings.Contains(sender.card, "comment_id=comment-1") || !strings.Contains(sender.card, "tab=review-meeting") || !strings.Contains(sender.card, "week=2026-W35") || len(sender.idempotencyKey) > 50 || !strings.HasPrefix(sender.idempotencyKey, "okr-cmt-") {
		t.Fatalf("card = %q, idempotency key = %q", sender.card, sender.idempotencyKey)
	}
	if sender.openID != "bob@example.test" || sender.authorEmail != "alice@example.com" {
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
		Recipient: CommentMention{Email: "owner@example.test", Name: "负责人"},
		CommentID: "comment-plan", AuthorName: "张若怡", Quarter: "2026-Q4", PlanID: "plan-1",
		PlanTitle: "2026 Q4 Biz OKR Plan", ObjectiveTitle: "O 原文", KRTitle: "KR 原文",
		OriginalText: "指标原文", Content: "@负责人 请确认", Tab: commentSourceTabOKRPlan,
	}
	if _, err := notifier.NotifyCommentMention(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"张若怡 @ 了你", "页面：Biz OKR Plan", "Plan：2026 Q4 Biz OKR Plan", "**原文：**指标原文", "**评论：**@负责人 请确认",
		"quarter=2026-Q4", "plan_id=plan-1", "tab=okr-plan", "comment_id=comment-plan",
	} {
		if !strings.Contains(sender.card, expected) {
			t.Fatalf("card %q does not contain %q", sender.card, expected)
		}
	}
}

func TestBotCommentOwnerNotificationUsesOwnerCopy(t *testing.T) {
	sender := &commentBroadcastSenderStub{}
	notifier, err := NewBotCommentMentionNotifier(sender, "https://emily.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	input := CommentMentionNotification{
		Recipient: CommentMention{Email: "owner@example.test", Name: "负责人"},
		Reason:    commentNotificationReasonOwner, OwnerLevel: "point", PointKind: domain.PointKindProduct,
		CommentID: "comment-owner", AuthorName: "张若怡", Quarter: "2026-Q4", Week: "2026-W40",
		ObjectiveTitle: "O 原文", KRTitle: "KR 原文", PointTitle: "产品具体 KR",
		OriginalText: "产品原文", Content: "请确认", Tab: commentSourceTabReviewMeeting,
	}
	if _, err := notifier.NotifyCommentMention(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sender.card, "张若怡 评论了你负责的产品具体 KR") || strings.Contains(sender.card, "张若怡 @ 了你") {
		t.Fatalf("owner card = %s", sender.card)
	}
}
