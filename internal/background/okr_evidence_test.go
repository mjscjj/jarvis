package background

import (
	"errors"
	"testing"
	"time"

	"jarvis/internal/domain"
)

func TestApplyOKREvidencePreservesFactWhenPageCASConflicts(t *testing.T) {
	db := openBackgroundTestDB(t)
	projects, err := NewProjectService(db)
	if err != nil {
		t.Fatal(err)
	}
	project, err := projects.Create(t.Context(), ProjectInput{
		Name: "发布", Role: "owner", Status: "active", Priority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	message := domain.Message{
		MessageID: "om_evidence_1", ChatID: "oc_project", ChatMode: "group",
		SenderOpenID: "ou_owner", SenderName: "Owner", SenderType: "user",
		MessageType: "text", Content: "容量审批已通过", CreateTime: 1,
		Source: "poll", RenderOK: true,
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	pages, err := NewPageService(db)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := pages.GetPage(t.Context(), PageTypeProject, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pages.UpdatePage(t.Context(), PageTypeProject, project.ID, UpdatePageInput{
		Content: "远端刚写入的结论", IfUnchangedSince: stale.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(2026, 8, 27, 3, 0, 0, 0, time.UTC)
	_, err = pages.ApplyOKREvidence(t.Context(), OKREvidenceInput{
		SubjectType: PageTypeProject, SubjectID: project.ID,
		EvidenceMessage: message.MessageID, Description: "容量审批已通过。", OccurredAt: occurredAt,
		Content: "基于审批证据合并后的结论", IfUnchangedSince: stale.UpdatedAt,
	})
	var conflict *OKREvidenceConflictError
	if !errors.As(err, &conflict) || conflict.Result == nil || conflict.Result.Page == nil || conflict.Result.Page.Summary != "远端刚写入的结论" {
		t.Fatalf("ApplyOKREvidence() error = %#v, want conflict with current Page", err)
	}
	var facts []domain.Fact
	if err := db.Where("subject_type = ? AND subject_id = ? AND source_kind = ? AND source_id = ?",
		PageTypeProject, project.ID, "message", message.ID).Find(&facts).Error; err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Description != "容量审批已通过。" {
		t.Fatalf("source-traceable facts = %#v, want evidence retained across Page conflict", facts)
	}
}

func TestListUnassociatedOKREvidenceFiltersAndExcludesOKRAssociations(t *testing.T) {
	db := openBackgroundTestDB(t)
	base := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	messages := []domain.Message{
		{MessageID: "clue:meego:wi-open", ChatID: "clue:meego", ChatMode: "clue", SenderOpenID: "__clue__", SenderName: "meego", SenderType: "system", MessageType: "clue", Content: "增长灰度进入验证", CreateTime: base.Add(time.Hour).UnixMilli(), Source: "clue", RenderOK: true},
		{MessageID: "clue:meego:wi-linked", ChatID: "clue:meego", ChatMode: "clue", SenderOpenID: "__clue__", SenderName: "meego", SenderType: "system", MessageType: "clue", Content: "增长发布完成", CreateTime: base.Add(2 * time.Hour).UnixMilli(), Source: "clue", RenderOK: true},
		{MessageID: "om_message_open", ChatID: "oc_growth", ChatMode: "group", SenderOpenID: "ou_owner", SenderName: "Owner", SenderType: "user", MessageType: "text", Content: "增长风险仍待处理", CreateTime: base.Add(3 * time.Hour).UnixMilli(), Source: "poll", RenderOK: true},
		{MessageID: "clue:lark_doc:doc-open", ChatID: "clue:lark_doc", ChatMode: "clue", SenderOpenID: "__clue__", SenderName: "lark_doc", SenderType: "system", MessageType: "clue", Content: "其他材料已更新", CreateTime: base.Add(4 * time.Hour).UnixMilli(), Source: "clue", RenderOK: true},
	}
	if err := db.Create(&messages).Error; err != nil {
		t.Fatal(err)
	}
	meego := "meego"
	if err := db.Create(&domain.Fact{
		SubjectType: PageTypeProject, SubjectID: 7, Description: "增长发布完成",
		OccurredAt: base.Add(2 * time.Hour), SourceKind: &meego, SourceID: &messages[1].ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// A same-source Fact on a non-OKR world entity must not consume the message
	// from this deliberate OKR-association queue.
	messageSource := "message"
	if err := db.Create(&domain.Fact{
		SubjectType: PageTypeGroup, SubjectID: 9, Description: "群背景事实",
		OccurredAt: base.Add(3 * time.Hour), SourceKind: &messageSource, SourceID: &messages[2].ID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	pages, err := NewPageService(db)
	if err != nil {
		t.Fatal(err)
	}
	from, until := base, base.Add(4*time.Hour)
	result, err := pages.ListUnassociatedOKREvidence(t.Context(), UnassociatedOKREvidenceFilter{
		Source: "meego", Anchor: "增长", From: &from, Until: &until, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 || len(result.Items) != 1 || result.Items[0].EvidenceMessageID != messages[0].MessageID ||
		result.Items[0].SourceKind != "meego" || !result.Items[0].CapturedAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("Meego unassociated evidence = %#v", result)
	}

	regular, err := pages.ListUnassociatedOKREvidence(t.Context(), UnassociatedOKREvidenceFilter{
		Source: "message", Anchor: "增长", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if regular.Count != 1 || regular.Items[0].EvidenceMessageID != messages[2].MessageID {
		t.Fatalf("message evidence = %#v, want non-OKR Fact to remain visible", regular)
	}
	literal, err := pages.ListUnassociatedOKREvidence(t.Context(), UnassociatedOKREvidenceFilter{
		Source: "meego", Anchor: "%", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if literal.Count != 0 {
		t.Fatalf("literal wildcard anchor matched unrelated evidence: %#v", literal)
	}
}

func TestListUnassociatedOKREvidenceValidatesFilter(t *testing.T) {
	pages, err := NewPageService(openBackgroundTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	until := from.Add(-time.Hour)
	for _, filter := range []UnassociatedOKREvidenceFilter{
		{Limit: 0},
		{Limit: 20, Source: "Meego"},
		{Limit: 20, Source: "Meego:all"},
		{Limit: 20, From: &from, Until: &until},
	} {
		if _, err := pages.ListUnassociatedOKREvidence(t.Context(), filter); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("filter %#v error = %v, want ErrInvalidInput", filter, err)
		}
	}
}
