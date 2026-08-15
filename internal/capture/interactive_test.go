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

func extractableForCaptureTest(message *domain.Message) bool {
	return message.RenderOK && !message.ExtractionSkipped
}
