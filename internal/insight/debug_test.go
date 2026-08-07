package insight

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newWatermarkDebugService(t *testing.T) (*DebugService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Message{}, &domain.TodoExtractWatermark{}); err != nil {
		t.Fatal(err)
	}
	return &DebugService{db: db}, db
}

func TestWatermarksIncludesLastMessageContent(t *testing.T) {
	service, db := newWatermarkDebugService(t)
	groupName := "Agent 核心群"
	group := domain.Group{ChatID: "oc_agent", ChatMode: "group", Name: &groupName}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	message := domain.Message{
		MessageID: "om_last", ChatID: group.ChatID, GroupID: &group.ID, ChatMode: "group",
		SenderOpenID: "ou_sender", SenderName: "张三", SenderType: "user", MessageType: "text",
		Content: "请检查今天的抽取链路\n并给出结论", CreateTime: 1, Source: "poll", RenderOK: true,
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	scannedAt := time.Date(2026, 8, 7, 9, 30, 0, 0, time.UTC)
	mark := domain.TodoExtractWatermark{
		ChatID: group.ChatID, LastScannedMessageID: message.MessageID,
		LastScannedAt: scannedAt, UpdatedAt: scannedAt.Add(time.Minute),
	}
	if err := db.Create(&mark).Error; err != nil {
		t.Fatal(err)
	}

	rows, err := service.Watermarks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].GroupName != groupName || rows[0].LastMessageID != message.MessageID || rows[0].LastMessageContent != message.Content {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestWatermarksFailsWhenReferencedMessageIsMissing(t *testing.T) {
	service, db := newWatermarkDebugService(t)
	mark := domain.TodoExtractWatermark{
		ChatID: "oc_missing", LastScannedMessageID: "om_missing",
		LastScannedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&mark).Error; err != nil {
		t.Fatal(err)
	}

	_, err := service.Watermarks(t.Context())
	if err == nil || !strings.Contains(err.Error(), "load extract watermark message chat_id=oc_missing message_id=om_missing") {
		t.Fatalf("error = %v", err)
	}
}
