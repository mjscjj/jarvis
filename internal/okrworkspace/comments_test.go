package okrworkspace

import "testing"

import "jarvis/internal/okrworkspace/domain"

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
