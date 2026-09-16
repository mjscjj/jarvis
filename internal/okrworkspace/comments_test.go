package okrworkspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"jarvis/internal/datatypes"
	"jarvis/internal/okrworkspace/domain"
)

type commentMentionNotifierStub struct {
	items []CommentMentionNotification
	err   error
}

func (stub *commentMentionNotifierStub) NotifyCommentMention(_ context.Context, notification CommentMentionNotification) (string, error) {
	stub.items = append(stub.items, notification)
	return "om_test_notice", stub.err
}

func TestCommentRecordsAuthorUnionIDThroughReadBack(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", TargetType: "kr", TargetID: "kr1",
		AuthorOpenID: "ou_alice", AuthorUnionID: "on_alice", AuthorName: "Alice",
		Content: "这个 KR 的口径要对一下",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.AuthorUnionID != "on_alice" || created.AuthorOpenID != "ou_alice" || created.AuthorName != "Alice" {
		t.Fatalf("created comment lost its author: %#v", created)
	}

	list, err := service.Comments(t.Context(), "2026-Q3", "2026-W35")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Comments) != 1 {
		t.Fatalf("comments = %#v", list)
	}
	if got := list.Comments[0]; got.AuthorUnionID != "on_alice" || got.AuthorName != "Alice" {
		t.Fatalf("author did not survive read back: %#v", got)
	}
}

func TestCommentTodoAndResolutionAreIndependentRootActions(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	root, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", TargetType: "kr", TargetID: "kr1", Content: "下周确认区域名单",
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", ParentID: root.ID, Content: "收到",
	})
	if err != nil {
		t.Fatal(err)
	}

	todo := true
	updated, err := service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{ExpectedVersion: root.Version, Todo: &todo})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Todo || updated.Resolved || updated.Content != "下周确认区域名单" {
		t.Fatalf("todo should preserve content and resolution: %#v", updated)
	}

	resolved := true
	updated, err = service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{ExpectedVersion: updated.Version, Resolved: &resolved})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Todo || !updated.Resolved || updated.Content != "下周确认区域名单" {
		t.Fatalf("resolution should preserve todo and content: %#v", updated)
	}
	if _, err := service.UpdateComment(t.Context(), reply.ID, UpdateCommentInput{ExpectedVersion: reply.Version, Todo: &todo}); err == nil {
		t.Fatal("reply unexpectedly accepted todo marker")
	}
	if _, err := service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{}); err == nil {
		t.Fatal("empty comment patch unexpectedly succeeded")
	}

	list, err := service.Comments(t.Context(), "2026-Q3", "2026-W35")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Comments) != 1 || !list.Comments[0].Todo || !list.Comments[0].Resolved {
		t.Fatalf("todo or resolved state did not persist: %#v", list)
	}
}

func TestCommentContentPatchStillValidatesText(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	comment, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", Content: "原评论",
	})
	if err != nil {
		t.Fatal(err)
	}
	content := "  修改后的评论  "
	updated, err := service.UpdateComment(t.Context(), comment.ID, UpdateCommentInput{ExpectedVersion: comment.Version, Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != "修改后的评论" {
		t.Fatalf("updated content = %q", updated.Content)
	}
	empty := "   "
	if _, err := service.UpdateComment(t.Context(), comment.ID, UpdateCommentInput{ExpectedVersion: updated.Version, Content: &empty}); err == nil {
		t.Fatal("blank comment content unexpectedly succeeded")
	}
}

func TestCommentEditRejectsAStaleVersion(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{Quarter: "2026-Q3", Week: "2026-W35", Content: "原评论"})
	if err != nil {
		t.Fatal(err)
	}
	first := "第一位填写者的修改"
	updated, err := service.UpdateComment(t.Context(), created.ID, UpdateCommentInput{ExpectedVersion: created.Version, Content: &first})
	if err != nil {
		t.Fatal(err)
	}
	second := "旧页面的覆盖"
	if _, err := service.UpdateComment(t.Context(), created.ID, UpdateCommentInput{ExpectedVersion: created.Version, Content: &second}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale comment update error = %v, want ErrConflict", err)
	}
	if updated.Version != created.Version+1 || updated.Content != first {
		t.Fatalf("updated comment = %#v", updated)
	}
}

func TestCommentDeleteRejectsAStaleThreadSnapshot(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	root, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{Quarter: "2026-Q3", Week: "2026-W35", Content: "根评论"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{Quarter: "2026-Q3", Week: "2026-W35", ParentID: root.ID, Content: "另一位填写者刚添加的回复"}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteComment(t.Context(), root.ID, DeleteCommentInput{ExpectedVersion: root.Version, DeleteToken: root.DeleteToken}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale thread delete error = %v, want ErrConflict", err)
	}
	list, err := service.Comments(t.Context(), "2026-Q3", "2026-W35")
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 2 || len(list.Comments) != 1 || len(list.Comments[0].Replies) != 1 {
		t.Fatalf("stale delete changed thread: %#v", list)
	}
	current := list.Comments[0]
	if err := service.DeleteComment(t.Context(), current.ID, DeleteCommentInput{ExpectedVersion: current.Version, DeleteToken: current.DeleteToken}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentSupportsUploadedImagesAndImageOnlyReplies(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	image := CommentImage{ID: "img-123", Name: "截图.png", URL: "/okr-assets/123.png", Width: 320}
	root, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", TargetType: "kr", TargetID: "kr1", Images: []CommentImage{image},
	})
	if err != nil {
		t.Fatal(err)
	}
	if root.Content != "" || len(root.Images) != 1 || root.Images[0].ID != image.ID {
		t.Fatalf("image-only root = %#v", root)
	}
	reply, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", ParentID: root.ID, Images: []CommentImage{image},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Images) != 1 || reply.TargetID != root.TargetID {
		t.Fatalf("image-only reply = %#v", reply)
	}
	empty := []CommentImage{}
	if _, err := service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{ExpectedVersion: root.Version, Images: &empty}); err == nil || !strings.Contains(err.Error(), "content or image") {
		t.Fatalf("removing the only image error = %v", err)
	}
	content := "补充文字"
	updated, err := service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{ExpectedVersion: root.Version, Content: &content, Images: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != content || len(updated.Images) != 0 {
		t.Fatalf("updated comment = %#v", updated)
	}
}

func TestFollowUpCommentRequiresAnExistingTargetInTheSameScope(t *testing.T) {
	db := openWorkspaceTestDB(t)
	for _, week := range []string{"2026-W35", "2026-W36"} {
		if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: week, OpenedBy: "test"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	followUp, err := service.CreateFollowUp(t.Context(), FollowUpInput{
		ID: "followup-comment-target", Quarter: "2026-Q3", Week: "2026-W36", Topic: "确认上线节奏",
		Status: domain.FollowUpStatusNotStarted, SourcePayload: datatypes.JSON(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	comment, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W36", TargetType: "follow_up", TargetID: followUp.ID,
		TargetTitle: followUp.Topic, AuthorOpenID: "ou_alice", AuthorName: "Alice", Content: "请补充具体日期",
	})
	if err != nil {
		t.Fatal(err)
	}
	if comment.TargetType != "follow_up" || comment.TargetID != followUp.ID || comment.TargetTitle != followUp.Topic {
		t.Fatalf("created comment lost its follow-up target: %#v", comment)
	}
	if _, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", TargetType: "follow_up", TargetID: followUp.ID, Content: "跨周评论",
	}); err == nil {
		t.Fatal("cross-week follow-up comment unexpectedly created")
	}
	if _, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W36", TargetType: "follow_up", TargetID: "missing", Content: "不存在",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing target error = %v, want ErrNotFound", err)
	}
}

func TestFollowUpCommentRepliesInheritTargetAndRemainAsHistoryAfterItemDeletion(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W36", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	followUp, err := service.CreateFollowUp(t.Context(), FollowUpInput{
		ID: "followup-history", Quarter: "2026-Q3", Week: "2026-W36", Topic: "保留讨论",
		Status: domain.FollowUpStatusInProgress, SourcePayload: datatypes.JSON(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W36", TargetType: "follow_up", TargetID: followUp.ID,
		TargetTitle: followUp.Topic, Content: "根评论",
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W36", ParentID: root.ID,
		TargetType: "kr", TargetID: "wrong-target", Content: "回复",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.TargetType != "follow_up" || reply.TargetID != followUp.ID || reply.TargetTitle != followUp.Topic {
		t.Fatalf("reply did not inherit follow-up target: %#v", reply)
	}
	if err := service.DeleteFollowUp(t.Context(), followUp.ID, DeleteFollowUpInput{ExpectedVersion: followUp.Version}); err != nil {
		t.Fatal(err)
	}
	list, err := service.Comments(t.Context(), "2026-Q3", "2026-W36")
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 2 || len(list.Comments) != 1 || len(list.Comments[0].Replies) != 1 {
		t.Fatalf("follow-up discussion was not retained as history: %#v", list)
	}
}

func TestPlanCommentsAreScopedValidatedAndDeletedWithPlan(t *testing.T) {
	db := openWorkspaceTestDB(t)
	for _, plan := range []domain.OKRPlan{
		{ID: "plan-comment-a", Quarter: "2026-Q3", Title: "Plan A"},
		{ID: "plan-comment-b", Quarter: "2026-Q3", Title: "Plan B"},
	} {
		if err := db.Create(&plan).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, objective := range []domain.Objective{
		{ID: "plan-comment-o-a", PlanID: "plan-comment-a", Quarter: "2026-Q3", Title: "O A"},
		{ID: "plan-comment-o-b", PlanID: "plan-comment-b", Quarter: "2026-Q3", Title: "O B"},
	} {
		if err := db.Create(&objective).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, kr := range []domain.KR{
		{ID: "plan-comment-kr-a", ObjectiveID: "plan-comment-o-a", Title: "KR A"},
		{ID: "plan-comment-kr-b", ObjectiveID: "plan-comment-o-b", Title: "KR B"},
	} {
		if err := db.Create(&kr).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	root, err := service.CreatePlanComment(t.Context(), "plan-comment-a", CreateCommentInput{
		TargetType: "kr", TargetID: "plan-comment-kr-a", TargetTitle: "KR A", Content: "需要补充口径",
	})
	if err != nil {
		t.Fatal(err)
	}
	if root.PlanID != "plan-comment-a" {
		t.Fatalf("created plan comment = %#v", root)
	}
	reply, err := service.CreatePlanComment(t.Context(), "plan-comment-a", CreateCommentInput{ParentID: root.ID, Content: "已补充"})
	if err != nil {
		t.Fatal(err)
	}
	if reply.TargetType != "kr" || reply.TargetID != "plan-comment-kr-a" || reply.PlanID != root.PlanID {
		t.Fatalf("reply did not inherit plan target: %#v", reply)
	}
	if _, err := service.CreatePlanComment(t.Context(), "plan-comment-a", CreateCommentInput{
		TargetType: "kr", TargetID: "plan-comment-kr-b", Content: "跨 Plan",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-plan target error = %v, want ErrNotFound", err)
	}

	list, err := service.PlanComments(t.Context(), "plan-comment-a")
	if err != nil {
		t.Fatal(err)
	}
	if list.PlanID != "plan-comment-a" || list.Quarter != "2026-Q3" || list.Week != "" || list.Count != 2 || len(list.Comments) != 1 || len(list.Comments[0].Replies) != 1 {
		t.Fatalf("plan comments = %#v", list)
	}
	empty, err := service.PlanComments(t.Context(), "plan-comment-b")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Count != 0 {
		t.Fatalf("other plan leaked comments: %#v", empty)
	}
	todo := true
	updated, err := service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{ExpectedVersion: root.Version, Todo: &todo})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Todo || updated.PlanID != root.PlanID {
		t.Fatalf("plan comment todo = %#v", updated)
	}
	list, err = service.PlanComments(t.Context(), "plan-comment-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Comments) != 1 || !list.Comments[0].Todo {
		t.Fatalf("plan comment todo did not persist: %#v", list)
	}

	planBeforeDelete, err := service.GetPlan(t.Context(), "plan-comment-a")
	if err != nil {
		t.Fatalf("get plan before delete: %v", err)
	}
	if err := service.DeletePlan(t.Context(), "plan-comment-a", planBeforeDelete.DeleteToken); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&domain.PageComment{}).Where("plan_id = ?", "plan-comment-a").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("deleted plan retained %d comments", count)
	}
}

func TestRegionalAlignmentCommentAcceptsTodo(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.OKRPlan{ID: "regional-comment-plan", Quarter: "2026-Q4", Title: "Regional Plan"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	root, err := service.CreateAlignmentComment(t.Context(), "2026-Q4", "eu", CreateCommentInput{Content: "确认区域上线范围"})
	if err != nil {
		t.Fatal(err)
	}
	todo := true
	updated, err := service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{ExpectedVersion: root.Version, Todo: &todo})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Todo || updated.AlignmentID == "" || updated.RegionCode != "eu" {
		t.Fatalf("regional alignment comment todo = %#v", updated)
	}
	list, err := service.AlignmentComments(t.Context(), "2026-Q4", "eu")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Comments) != 1 || !list.Comments[0].Todo {
		t.Fatalf("regional alignment comment todo did not persist: %#v", list)
	}
}

func TestCommentMentionNotifiesWithWeekObjectiveKRAndExactContent(t *testing.T) {
	db := openWorkspaceTestDB(t)
	week := domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "test"}
	objective := domain.Objective{ID: "mention-o", Quarter: week.Quarter, Title: "提升平台经营效率"}
	kr := domain.KR{ID: "mention-kr", ObjectiveID: objective.ID, Title: "完成核心工具升级"}
	point := domain.KRPoint{ID: "mention-point", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "交付 AM 助手"}
	for _, row := range []any{&week, &objective, &kr, &point} {
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
	content := "@张若怡 请核对这条进展\n不要遗漏原文。"
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: week.Quarter, Week: week.Week, TargetType: "point", TargetID: point.ID,
		AuthorOpenID: "ou_alice", AuthorName: "Alice", Content: content,
		Mentions: []CommentMention{{Email: "zhangruoyi@example.test", Name: "张若怡"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Mentions) != 1 || len(stub.items) != 1 {
		t.Fatalf("created = %#v, notifications = %#v", created, stub.items)
	}
	got := stub.items[0]
	if got.Recipient.Email != "zhangruoyi@example.test" || got.AuthorName != "Alice" || got.Week != week.Week || got.ObjectiveTitle != objective.Title || got.KRTitle != kr.Title || got.Content != content || got.Tab != "review-fill" {
		t.Fatalf("notification = %#v", got)
	}
}

func TestCommentMentionFailureKeepsCommentAndReportsDeliveryError(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	stub := &commentMentionNotifierStub{err: fmt.Errorf("飞书返回 403")}
	if err := service.SetCommentMentionNotifier(stub); err != nil {
		t.Fatal(err)
	}
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", Content: "@Bob 请确认",
		Mentions: []CommentMention{{Email: "bob@example.test", Name: "Bob"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.NotificationErrors) != 1 || !strings.Contains(created.NotificationErrors[0], "待核验") || strings.Contains(created.NotificationErrors[0], "403") {
		t.Fatalf("notification errors = %#v", created.NotificationErrors)
	}
	list, err := service.Comments(t.Context(), "2026-Q3", "2026-W35")
	if err != nil || list.Count != 1 {
		t.Fatalf("persisted comment = %#v, err = %v", list, err)
	}
}

func TestCommentMentionRequiresSelectedTokenAndEditsDoNotNotify(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	stub := &commentMentionNotifierStub{}
	if err := service.SetCommentMentionNotifier(stub); err != nil {
		t.Fatal(err)
	}
	if _, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", Content: "没有 token",
		Mentions: []CommentMention{{Email: "bob@example.test", Name: "Bob"}},
	}); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("missing mention token error = %v", err)
	}
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", AuthorName: "Alice", Content: "@Bob 初次提醒",
		Mentions: []CommentMention{{Email: "bob@example.test", Name: "Bob"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	content := "@Bob 已提醒，@Carol 新加入"
	mentions := []CommentMention{{Email: "bob@example.test", Name: "Bob"}, {Email: "carol@example.test", Name: "Carol"}}
	if _, err := service.UpdateComment(t.Context(), created.ID, UpdateCommentInput{
		ExpectedVersion: created.Version, Content: &content, Mentions: &mentions,
	}); err != nil {
		t.Fatal(err)
	}
	if len(stub.items) != 1 {
		t.Fatalf("notifications = %#v", stub.items)
	}
	list, err := service.Comments(t.Context(), "2026-Q3", "2026-W35")
	if err != nil || len(list.Comments) != 1 || len(list.Comments[0].Mentions) != 2 {
		t.Fatalf("edited mentions were not persisted: list=%#v err=%v", list, err)
	}
}

func TestReviewMeetingPointCommentNotifiesKROwnersAndDeduplicatesExplicitMention(t *testing.T) {
	db := openWorkspaceTestDB(t)
	week := domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "test"}
	objective := domain.Objective{ID: "owner-o", Quarter: week.Quarter, Title: "提升经营效率"}
	kr := domain.KR{ID: "owner-kr", ObjectiveID: objective.ID, Title: "完成核心工具升级"}
	point := domain.KRPoint{ID: "owner-point", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "交付 AM 助手完整原文"}
	owners := []domain.KROwner{
		{KRID: kr.ID, PersonID: 1, OwnerKey: "owner-a", Email: "owner_a@example.test", Name: "负责人甲", SortOrder: 0},
		{KRID: kr.ID, PersonID: 2, OwnerKey: "owner-b", Email: "owner_b@example.test", Name: "负责人乙", SortOrder: 1},
	}
	for _, row := range []any{&week, &objective, &kr, &point, &owners} {
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
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: week.Quarter, Week: week.Week, SourceTab: commentSourceTabReviewMeeting,
		TargetType: "point", TargetID: point.ID, TargetTitle: point.Title,
		SelectedText: "AM 助手", SelectionStart: 3, SelectionEnd: 8,
		AuthorOpenID: "ou_author", AuthorName: "张若怡", Content: "@负责人乙 请补充验收结果",
		Mentions: []CommentMention{{Email: "owner_b@example.test", Name: "负责人乙"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.NotificationErrors) != 0 || len(stub.items) != 2 {
		t.Fatalf("created = %#v, notifications = %#v", created, stub.items)
	}
	byEmail := map[string]CommentMentionNotification{}
	for _, notification := range stub.items {
		byEmail[notification.Recipient.Email] = notification
	}
	if byEmail["owner_a@example.test"].Reason != commentNotificationReasonOwner || byEmail["owner_b@example.test"].Reason != commentNotificationReasonMentionAndOwner {
		t.Fatalf("notification reasons = %#v", byEmail)
	}
	notification := byEmail["owner_b@example.test"]
	if notification.OwnerLevel != "kr" || notification.Tab != commentSourceTabReviewMeeting || notification.KRID != kr.ID || notification.KRTitle != kr.Title || notification.ObjectiveTitle != objective.Title || notification.OriginalText != "AM 助手" {
		t.Fatalf("notification context = %#v", notification)
	}
}

func TestReviewMeetingCommentResolvesWeekSeededMetricToItsKR(t *testing.T) {
	db := openWorkspaceTestDB(t)
	week := domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "test"}
	objective := domain.Objective{ID: "weekly-metric-o", Quarter: week.Quarter, Title: "提升经营效率"}
	kr := domain.KR{ID: "weekly-metric-kr", ObjectiveID: objective.ID, Title: "完成核心工具升级"}
	core := domain.WeeklyKRCore{KRID: kr.ID, Week: week.Week, Metrics: []domain.WeeklyMetric{{ID: "week-only-metric", Text: "本周实际覆盖率 86%"}}}
	owner := domain.KROwner{KRID: kr.ID, PersonID: 1, OwnerKey: "owner", Email: "weekly_owner@example.test", Name: "周度负责人"}
	for _, row := range []any{&week, &objective, &kr, &core, &owner} {
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
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: week.Quarter, Week: week.Week, SourceTab: commentSourceTabReviewMeeting,
		TargetType: "metric", TargetID: "week-only-metric", TargetTitle: "客户端快照",
		AuthorOpenID: "ou_author", AuthorName: "张若怡", Content: "@周度负责人 请核对覆盖率",
		Mentions: []CommentMention{{Email: owner.Email, Name: owner.Name}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.NotificationErrors) != 0 || len(stub.items) != 1 || stub.items[0].KRID != kr.ID || stub.items[0].OriginalText != "本周实际覆盖率 86%" {
		t.Fatalf("created = %#v, notifications = %#v", created, stub.items)
	}
}

func TestPlanPointCommentWithoutMentionNotifiesNearestPointOwners(t *testing.T) {
	db := openWorkspaceTestDB(t)
	plan := domain.OKRPlan{ID: "point-owner-plan", Quarter: "2026-Q4", Title: "2026 Q4 Biz OKR Plan"}
	objective := domain.Objective{ID: "point-owner-o", PlanID: plan.ID, Quarter: plan.Quarter, Title: "扩大业务增长"}
	kr := domain.KR{ID: "point-owner-kr", ObjectiveID: objective.ID, Title: "提升平台收益"}
	point := domain.KRPoint{ID: "point-owner-point", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "优化激励策略"}
	krOwner := domain.KROwner{KRID: kr.ID, PersonID: 1, OwnerKey: "parent", Email: "parent@example.test", Name: "上层负责人"}
	pointOwners := []domain.PointOwner{
		{PointID: point.ID, PersonID: 2, OwnerKey: "point-a", Email: "point_a@example.test", Name: "具体负责人甲", SortOrder: 0},
		{PointID: point.ID, PersonID: 3, OwnerKey: "point-b", Email: "point_b@example.test", Name: "具体负责人乙", SortOrder: 1},
	}
	for _, row := range []any{&plan, &objective, &kr, &point, &krOwner, &pointOwners} {
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
	created, err := service.CreatePlanComment(t.Context(), plan.ID, CreateCommentInput{
		TargetType: "point", TargetID: point.ID, TargetTitle: point.Title,
		AuthorOpenID: "ou_author", AuthorName: "张若怡", Content: "请确认策略",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.processPendingCommentDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(created.NotificationErrors) != 0 || len(stub.items) != 2 {
		t.Fatalf("created = %#v, notifications = %#v", created, stub.items)
	}
	for _, notification := range stub.items {
		if notification.OwnerLevel != "point" || notification.Reason != commentNotificationReasonOwner || notification.Recipient.Email == krOwner.Email {
			t.Fatalf("point owner notification = %#v", notification)
		}
	}
}

func TestPointOwnerAndExplicitMentionAreUnionedWithoutNotifyingParentOwner(t *testing.T) {
	db := openWorkspaceTestDB(t)
	plan := domain.OKRPlan{ID: "union-plan", Quarter: "2026-Q4", Title: "2026 Q4 Plan"}
	objective := domain.Objective{ID: "union-o", PlanID: plan.ID, Quarter: plan.Quarter, Title: "增长"}
	kr := domain.KR{ID: "union-kr", ObjectiveID: objective.ID, Title: "提升收益"}
	point := domain.KRPoint{ID: "union-point", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "交付商业化方案"}
	pointOwner := domain.PointOwner{PointID: point.ID, PersonID: 1, OwnerKey: "point", Email: "point@example.test", Name: "具体负责人"}
	krOwner := domain.KROwner{KRID: kr.ID, PersonID: 2, OwnerKey: "kr", Email: "kr@example.test", Name: "KR 负责人"}
	for _, row := range []any{&plan, &objective, &kr, &point, &pointOwner, &krOwner} {
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
	_, err = service.CreatePlanComment(t.Context(), plan.ID, CreateCommentInput{
		TargetType: "point", TargetID: point.ID, AuthorName: "Alice",
		Content: "@审阅人 请一起核对", Mentions: []CommentMention{{Email: "reviewer@example.test", Name: "审阅人"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.processPendingCommentDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(stub.items) != 2 {
		t.Fatalf("notifications = %#v", stub.items)
	}
	byEmail := map[string]CommentMentionNotification{}
	for _, notification := range stub.items {
		byEmail[notification.Recipient.Email] = notification
	}
	if byEmail[pointOwner.Email].Reason != commentNotificationReasonOwner || byEmail["reviewer@example.test"].Reason != commentNotificationReasonMention || byEmail[krOwner.Email].Recipient.Email != "" {
		t.Fatalf("union notifications = %#v", byEmail)
	}
}

func TestCommentFallsBackFromPointAndKRToObjectiveOwner(t *testing.T) {
	db := openWorkspaceTestDB(t)
	week := domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}
	objective := domain.Objective{ID: "fallback-o", Quarter: week.Quarter, Title: "提升效率"}
	kr := domain.KR{ID: "fallback-kr", ObjectiveID: objective.ID, Title: "完成升级"}
	point := domain.KRPoint{ID: "fallback-point", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "制定策略"}
	owner := domain.ObjectiveOwner{ObjectiveID: objective.ID, PersonID: 1, OwnerKey: "objective", Email: "objective@example.test", Name: "O 负责人"}
	for _, row := range []any{&week, &objective, &kr, &point, &owner} {
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
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: week.Quarter, Week: week.Week, TargetType: "point", TargetID: point.ID,
		AuthorName: "Alice", Content: "请补充验收口径",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Notifications) != 1 || len(stub.items) != 1 || stub.items[0].Recipient.Email != owner.Email || stub.items[0].OwnerLevel != "objective" || stub.items[0].Reason != commentNotificationReasonOwner {
		t.Fatalf("fallback notification = %#v / %#v", created, stub.items)
	}
}

func TestPlanCommentNotifiesExplicitMentionWithCanonicalTargetText(t *testing.T) {
	db := openWorkspaceTestDB(t)
	plan := domain.OKRPlan{ID: "owner-plan", Quarter: "2026-Q4", Title: "2026 Q4 Biz OKR Plan"}
	objective := domain.Objective{ID: "owner-plan-o", PlanID: plan.ID, Quarter: plan.Quarter, Title: "扩大业务增长"}
	kr := domain.KR{ID: "owner-plan-kr", ObjectiveID: objective.ID, Title: "交付增长方案"}
	metric := domain.KRMetric{ID: "owner-plan-metric", KRID: kr.ID, Text: "覆盖率达到 90%"}
	owner := domain.KROwner{KRID: kr.ID, PersonID: 1, OwnerKey: "owner", Email: "plan_owner@example.test", Name: "Plan 负责人"}
	for _, row := range []any{&plan, &objective, &kr, &metric, &owner} {
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
	created, err := service.CreatePlanComment(t.Context(), plan.ID, CreateCommentInput{
		TargetType: "metric", TargetID: metric.ID, TargetTitle: "客户端可能过期的文本",
		AuthorOpenID: "ou_author", AuthorName: "张若怡", Content: "@Plan 负责人 请确认目标值",
		Mentions: []CommentMention{{Email: owner.Email, Name: owner.Name}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.processPendingCommentDeliveries(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(created.NotificationErrors) != 0 || len(stub.items) != 1 {
		t.Fatalf("created = %#v, notifications = %#v", created, stub.items)
	}
	got := stub.items[0]
	if got.Recipient.Email != owner.Email || got.Reason != commentNotificationReasonMentionAndOwner || got.OwnerLevel != "kr" || got.Tab != commentSourceTabOKRPlan || got.PlanTitle != plan.Title || got.KRID != kr.ID || got.OriginalText != metric.Text {
		t.Fatalf("plan owner notification = %#v", got)
	}
}

func TestCommentWithoutMentionNeverNotifies(t *testing.T) {
	db := openWorkspaceTestDB(t)
	week := domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "test"}
	if err := db.Create(&week).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	stub := &commentMentionNotifierStub{}
	if err := service.SetCommentMentionNotifier(stub); err != nil {
		t.Fatal(err)
	}
	if _, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: week.Quarter, Week: week.Week, SourceTab: commentSourceTabReviewFill,
		TargetType: "page", TargetID: "page", Content: "填写页评论",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: week.Quarter, Week: week.Week, SourceTab: commentSourceTabReviewMeeting,
		TargetType: "page", TargetID: "page", Content: "会议页评论",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.items) != 0 || len(created.NotificationErrors) != 0 {
		t.Fatalf("created = %#v, notifications = %#v", created, stub.items)
	}
}

func TestCommentRejectsUnknownSourceTabBeforePersisting(t *testing.T) {
	db := openWorkspaceTestDB(t)
	week := domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}
	if err := db.Create(&week).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = createAndDeliverCommentForTest(service, t.Context(), CreateCommentInput{
		Quarter: week.Quarter, Week: week.Week, SourceTab: "invented-tab", Content: "不会保存",
	})
	if err == nil || !strings.Contains(err.Error(), "source_tab") {
		t.Fatalf("unknown source_tab error = %v", err)
	}
	var count int64
	if err := db.Model(&domain.PageComment{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("persisted comments = %d, err = %v", count, err)
	}
}

func (stub *commentMentionNotifierStub) AppID() string { return "test-app" }
func (stub *commentMentionNotifierStub) VerifyCommentMention(context.Context, string) error {
	return stub.err
}

func createAndDeliverCommentForTest(service *Service, ctx context.Context, input CreateCommentInput) (CommentView, error) {
	view, err := service.CreateComment(ctx, input)
	if err != nil {
		return view, err
	}
	view.Notifications, err = service.deliverComment(ctx, view.ID, "")
	view.NotificationErrors = deliveryWarnings(view.Notifications)
	return view, err
}
