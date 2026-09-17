package okrworkspace

import (
	"errors"
	"testing"
)

func TestProductFeedbackCollaborationLifecycle(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	alice := FeedbackActor{UnionID: "on_alice", Email: "alice@example.com", Name: "Alice", AvatarURL: "https://img.example/alice.png"}
	bob := FeedbackActor{UnionID: "on_bob", Email: "bob@example.com", Name: "Bob", AvatarURL: "https://img.example/bob.png"}
	created, err := service.CreateProductFeedback(t.Context(), alice, CreateProductFeedbackInput{
		Title: "按钮无法保存", Content: "点击保存后没有任何提示。", SourceContext: []byte(`{"view_state":{"tab":"okr-plan","quarter":"2026-Q3"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.Author.Name != "Alice" || created.Author.AvatarURL != alice.AvatarURL || !created.CanResolve {
		t.Fatalf("created feedback = %#v", created)
	}

	updated, err := service.ReplyProductFeedback(t.Context(), bob, created.ID, "我也遇到了")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Replies) != 1 || updated.Replies[0].Author.Name != "Bob" || updated.Replies[0].Author.AvatarURL != bob.AvatarURL {
		t.Fatalf("reply did not retain author snapshot: %#v", updated.Replies)
	}

	updated, err = service.AddProductFeedbackPlusOne(t.Context(), bob, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err = service.AddProductFeedbackPlusOne(t.Context(), bob, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.MyPlusOne || len(updated.PlusOnes) != 1 || updated.PlusOnes[0].Name != "Bob" {
		t.Fatalf("idempotent plus one = %#v", updated)
	}

	if _, err := service.SetProductFeedbackResolved(t.Context(), bob, created.ID, created.Version, true); !errors.Is(err, ErrFeedbackForbidden) {
		t.Fatalf("non-owner resolve error = %v", err)
	}
	manager := bob
	manager.CanManage = true
	resolved, err := service.SetProductFeedbackResolved(t.Context(), manager, created.ID, created.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Resolved || resolved.Version != 2 || resolved.ResolvedBy == nil || resolved.ResolvedBy.Name != "Bob" {
		t.Fatalf("resolved feedback = %#v", resolved)
	}
	if _, err := service.SetProductFeedbackResolved(t.Context(), alice, created.ID, created.Version, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale reopen error = %v", err)
	}

	open, err := service.ProductFeedbacks(t.Context(), alice, ProductFeedbackQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	history, err := service.ProductFeedbacks(t.Context(), alice, ProductFeedbackQuery{Resolved: true, Sort: "popular", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if open.Total != 0 || history.Total != 1 || len(history.Items) != 1 || len(history.Items[0].Replies) != 1 || len(history.Items[0].PlusOnes) != 1 {
		t.Fatalf("lists open=%#v history=%#v", open, history)
	}
}

func TestProductFeedbackPlusOneCanBeRemovedAndOwnerCanReopen(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	actor := FeedbackActor{OpenID: "ou_owner", Name: "Owner"}
	created, err := service.CreateProductFeedback(t.Context(), actor, CreateProductFeedbackInput{Title: "建议", Content: "希望增加筛选"})
	if err != nil {
		t.Fatal(err)
	}
	liked, err := service.AddProductFeedbackPlusOne(t.Context(), actor, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	unliked, err := service.RemoveProductFeedbackPlusOne(t.Context(), actor, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !liked.MyPlusOne || len(liked.PlusOnes) != 1 || unliked.MyPlusOne || len(unliked.PlusOnes) != 0 {
		t.Fatalf("plus-one transition liked=%#v unliked=%#v", liked, unliked)
	}
	resolved, err := service.SetProductFeedbackResolved(t.Context(), actor, created.ID, created.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := service.SetProductFeedbackResolved(t.Context(), actor, created.ID, resolved.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Resolved || reopened.ResolvedAt != nil || reopened.ResolvedBy != nil || reopened.Version != 3 {
		t.Fatalf("reopened feedback = %#v", reopened)
	}
}
