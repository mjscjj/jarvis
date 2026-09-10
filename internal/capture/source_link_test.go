package capture

import (
	"encoding/json"
	"testing"
	"time"

	"jarvis/internal/domain"
)

func TestCapturePreservesAndEnrichesMessageLink(t *testing.T) {
	const link = "https://applink.feishu.cn/client/chat/open?openChatId=oc_group&position=42"
	var item CLIMessage
	if err := json.Unmarshal([]byte(`{"message_id":"om_trigger","message_app_link":"`+link+`","content":"请处理","msg_type":"text","create_time":"2026-09-10 10:00","sender":{"id":"ou_sender","name":"发起人","sender_type":"user"}}`), &item); err != nil {
		t.Fatal(err)
	}
	svc := &Service{opts: Options{Location: time.UTC}}
	message, err := svc.toDomainMessage(&domain.Group{ID: 1, ChatID: "oc_group", ChatMode: "group"}, item)
	if err != nil {
		t.Fatal(err)
	}
	if message.SourceURL == nil || *message.SourceURL != link {
		t.Fatalf("source URL lost: %#v", message.SourceURL)
	}
	db := newCaptureTestDB(t)
	original := *message
	original.SourceURL = nil
	if inserted, err := upsertMessage(db, &original); err != nil || !inserted {
		t.Fatalf("initial capture: %v %v", inserted, err)
	}
	if inserted, err := upsertMessage(db, message); err != nil || inserted {
		t.Fatalf("link enrichment: %v %v", inserted, err)
	}
	if inserted, err := upsertMessage(db, message); err != nil || inserted {
		t.Fatalf("repeat capture: %v %v", inserted, err)
	}
	var stored domain.Message
	if err := db.Where("message_id = ?", message.MessageID).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SourceURL == nil || *stored.SourceURL != link || stored.Content != original.Content {
		t.Fatalf("stored message: %#v", stored)
	}
}
