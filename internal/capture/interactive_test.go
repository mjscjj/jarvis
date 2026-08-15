package capture

import (
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCaptureInteractiveMarksMessageAsM3SkippedAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	service := &Service{db: db, now: time.Now}
	input := InteractiveMessage{
		MessageID: "om_direct", ChatID: "oc_group", ChatMode: "group", ChatName: "项目群",
		SenderOpenID: "ou_user", SenderName: "发起人", MessageType: "text",
		Content: "帮我查一下", ContentRaw: `{"text":"@_user_1 帮我查一下"}`,
		CreateTime: 1786752000000,
	}
	first, err := service.CaptureInteractive(t.Context(), input)
	if err != nil {
		t.Fatalf("CaptureInteractive() error = %v", err)
	}
	if !first.ExtractionSkipped || extractableForCaptureTest(first) {
		t.Fatalf("captured message = %#v", first)
	}
	second, err := service.CaptureInteractive(t.Context(), input)
	if err != nil {
		t.Fatalf("CaptureInteractive(redelivery) error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("redelivery created another row: first=%d second=%d", first.ID, second.ID)
	}
	var count int64
	if err := db.Model(&domain.Message{}).Where("message_id = ?", input.MessageID).Count(&count).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 1 {
		t.Fatalf("message count = %d, want 1", count)
	}
}

func TestCaptureInteractivePersistsFetchedFeishuHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	service := &Service{db: db, now: time.Now}
	input := InteractiveMessage{
		MessageID: "om_anchor", ChatID: "oc_topic", ChatMode: "group", ChatName: "话题群",
		SenderOpenID: "ou_user", SenderName: "发起人", MessageType: "text", Content: "当前请求",
		ContentRaw: `{"text":"当前请求"}`, ThreadID: "omt_topic", CreateTime: 3000,
		RecentMessages: []InteractiveHistoryMessage{
			{MessageID: "om_root", SenderOpenID: "ou_a", SenderName: "甲", SenderType: "user", MessageType: "text", Content: "话题根消息", ContentRaw: `{"text":"话题根消息"}`, ThreadID: "omt_topic", CreateTime: 1000},
			{MessageID: "om_reply", SenderOpenID: "ou_b", SenderName: "乙", SenderType: "user", MessageType: "text", Content: "上一条回复", ContentRaw: `{"text":"上一条回复"}`, RootID: "om_root", ThreadID: "omt_topic", CreateTime: 2000},
		},
	}
	if _, err := service.CaptureInteractive(t.Context(), input); err != nil {
		t.Fatalf("CaptureInteractive() error = %v", err)
	}
	var rows []domain.Message
	if err := db.Order("create_time ASC").Find(&rows).Error; err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("message count = %d, want 3", len(rows))
	}
	for index, wantID := range []string{"om_root", "om_reply", "om_anchor"} {
		if rows[index].MessageID != wantID || !rows[index].ExtractionSkipped || !rows[index].RenderOK {
			t.Fatalf("message[%d] = %#v", index, rows[index])
		}
	}
}

func TestCaptureInteractiveEnrichesExistingTopicMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Message{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	groupName := "话题群"
	group := &domain.Group{ChatID: "oc_topic", ChatMode: "group", Name: &groupName, CreatedAt: time.Now(), UpdatedAt: time.Now()}
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
	got, err := service.CaptureInteractive(t.Context(), InteractiveMessage{
		MessageID: existing.MessageID, ChatID: group.ChatID, ChatMode: "group", ChatName: groupName,
		SenderOpenID: existing.SenderOpenID, SenderName: existing.SenderName, MessageType: "text",
		Content: existing.Content, ContentRaw: raw, ParentID: "om_parent", RootID: "om_root",
		ThreadID: "omt_topic", CreateTime: existing.CreateTime,
	})
	if err != nil {
		t.Fatalf("CaptureInteractive() error = %v", err)
	}
	if !got.ExtractionSkipped || got.ReplyTo == nil || *got.ReplyTo != "om_parent" || got.RootID == nil || *got.RootID != "om_root" || got.ThreadID == nil || *got.ThreadID != "omt_topic" {
		t.Fatalf("captured message = %#v", got)
	}
	var persisted domain.Message
	if err := db.Where("message_id = ?", existing.MessageID).First(&persisted).Error; err != nil {
		t.Fatalf("reload message: %v", err)
	}
	if !persisted.ExtractionSkipped || persisted.ReplyTo == nil || *persisted.ReplyTo != "om_parent" || persisted.RootID == nil || *persisted.RootID != "om_root" || persisted.ThreadID == nil || *persisted.ThreadID != "omt_topic" {
		t.Fatalf("persisted message = %#v", persisted)
	}
}

func extractableForCaptureTest(message *domain.Message) bool {
	return message.RenderOK && !message.ExtractionSkipped
}
