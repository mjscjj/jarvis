package capture

import (
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCaptureRouteClaimSkipsOnlyCurrentMessageWithoutOpeningMonitoring(t *testing.T) {
	db := openRouteClaimTestDB(t)
	service := &Service{db: db, now: time.Now}
	input := RouteClaimMessage{
		MessageID: "om_direct", ChatID: "oc_group", ChatMode: "group", ChatName: "项目群",
		SenderOpenID: "ou_user", SenderName: "发起人", MessageType: "text",
		Content: "帮我查一下", ContentRaw: `{"text":"@_user_1 帮我查一下"}`,
		CreateTime: 1786752000000,
	}
	first, err := service.CaptureRouteClaim(t.Context(), input)
	if err != nil {
		t.Fatalf("CaptureRouteClaim() error = %v", err)
	}
	if !first.ExtractionSkipped || extractableForRouteClaimTest(first) {
		t.Fatalf("claimed message = %#v", first)
	}
	var group domain.Group
	if err := db.Where("chat_id = ?", input.ChatID).Take(&group).Error; err != nil {
		t.Fatalf("load group: %v", err)
	}
	if group.RelatedGroup || group.Tier != "cold" {
		t.Fatalf("route claim changed M2 monitoring: %#v", group)
	}

	second, err := service.CaptureRouteClaim(t.Context(), input)
	if err != nil {
		t.Fatalf("CaptureRouteClaim(redelivery) error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("redelivery created another row: first=%d second=%d", first.ID, second.ID)
	}
	var count int64
	if err := db.Model(&domain.Message{}).Count(&count).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("message count = %d, want 1", count)
	}
}

func TestCaptureRouteClaimMarksAndEnrichesExistingPolledMessage(t *testing.T) {
	db := openRouteClaimTestDB(t)
	groupName := "话题群"
	group := &domain.Group{ChatID: "oc_topic", ChatMode: "group", Name: &groupName, Tier: "hot", RelatedGroup: true}
	if err := db.Create(group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	raw := `{"text":"当前请求"}`
	existing := &domain.Message{
		MessageID: "om_anchor", ChatID: group.ChatID, GroupID: &group.ID, ChatMode: "group",
		SenderOpenID: "ou_user", SenderName: "发起人", SenderType: "user", MessageType: "text",
		Content: "当前请求", ContentRaw: &raw, CreateTime: 3000, Source: "poll", RenderOK: true,
	}
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("create existing message: %v", err)
	}
	service := &Service{db: db, now: time.Now}
	got, err := service.CaptureRouteClaim(t.Context(), RouteClaimMessage{
		MessageID: existing.MessageID, ChatID: group.ChatID, ChatMode: "group", ChatName: groupName,
		SenderOpenID: existing.SenderOpenID, SenderName: existing.SenderName, MessageType: "text",
		Content: existing.Content, ContentRaw: raw, ParentID: "om_parent", RootID: "om_root",
		ThreadID: "omt_topic", CreateTime: existing.CreateTime,
	})
	if err != nil {
		t.Fatalf("CaptureRouteClaim() error = %v", err)
	}
	if !got.ExtractionSkipped || got.ReplyTo == nil || *got.ReplyTo != "om_parent" || got.RootID == nil || *got.RootID != "om_root" || got.ThreadID == nil || *got.ThreadID != "omt_topic" {
		t.Fatalf("claimed message = %#v", got)
	}
	var persistedGroup domain.Group
	if err := db.Where("id = ?", group.ID).Take(&persistedGroup).Error; err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if !persistedGroup.RelatedGroup || persistedGroup.Tier != "hot" {
		t.Fatalf("route claim changed existing monitoring: %#v", persistedGroup)
	}
}

func openRouteClaimTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func extractableForRouteClaimTest(message *domain.Message) bool {
	return message.RenderOK && !message.ExtractionSkipped
}
