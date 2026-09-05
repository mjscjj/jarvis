package okrworkspace

import (
	"errors"
	"testing"

	"jarvis/internal/datatypes"
	"jarvis/internal/okrworkspace/domain"
)

func TestCommentRecordsAuthorUnionIDThroughReadBack(t *testing.T) {
	db := openWorkspaceTestDB(t)
	if err := db.Create(&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"}).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateComment(t.Context(), CreateCommentInput{
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
	root, err := service.CreateComment(t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", TargetType: "kr", TargetID: "kr1", Content: "下周确认区域名单",
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := service.CreateComment(t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", ParentID: root.ID, Content: "收到",
	})
	if err != nil {
		t.Fatal(err)
	}

	todo := true
	updated, err := service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{Todo: &todo})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Todo || updated.Resolved || updated.Content != "下周确认区域名单" {
		t.Fatalf("todo should preserve content and resolution: %#v", updated)
	}

	resolved := true
	updated, err = service.UpdateComment(t.Context(), root.ID, UpdateCommentInput{Resolved: &resolved})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Todo || !updated.Resolved || updated.Content != "下周确认区域名单" {
		t.Fatalf("resolution should preserve todo and content: %#v", updated)
	}
	if _, err := service.UpdateComment(t.Context(), reply.ID, UpdateCommentInput{Todo: &todo}); err == nil {
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
	comment, err := service.CreateComment(t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", Content: "原评论",
	})
	if err != nil {
		t.Fatal(err)
	}
	content := "  修改后的评论  "
	updated, err := service.UpdateComment(t.Context(), comment.ID, UpdateCommentInput{Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content != "修改后的评论" {
		t.Fatalf("updated content = %q", updated.Content)
	}
	empty := "   "
	if _, err := service.UpdateComment(t.Context(), comment.ID, UpdateCommentInput{Content: &empty}); err == nil {
		t.Fatal("blank comment content unexpectedly succeeded")
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
	comment, err := service.CreateComment(t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W36", TargetType: "follow_up", TargetID: followUp.ID,
		TargetTitle: followUp.Topic, AuthorOpenID: "ou_alice", AuthorName: "Alice", Content: "请补充具体日期",
	})
	if err != nil {
		t.Fatal(err)
	}
	if comment.TargetType != "follow_up" || comment.TargetID != followUp.ID || comment.TargetTitle != followUp.Topic {
		t.Fatalf("created comment lost its follow-up target: %#v", comment)
	}
	if _, err := service.CreateComment(t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W35", TargetType: "follow_up", TargetID: followUp.ID, Content: "跨周评论",
	}); err == nil {
		t.Fatal("cross-week follow-up comment unexpectedly created")
	}
	if _, err := service.CreateComment(t.Context(), CreateCommentInput{
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
	root, err := service.CreateComment(t.Context(), CreateCommentInput{
		Quarter: "2026-Q3", Week: "2026-W36", TargetType: "follow_up", TargetID: followUp.ID,
		TargetTitle: followUp.Topic, Content: "根评论",
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := service.CreateComment(t.Context(), CreateCommentInput{
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
