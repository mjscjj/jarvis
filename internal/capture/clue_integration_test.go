//go:build integration

package capture

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"
)

// TestAppendClueSQLite pins the delivery contract against SQLite: the channel is
// created on first use and lands inside M3's scan range, the clue is stored as
// an extractable message, and redelivery is a no-op that does not re-wake M3.
func TestAppendClueSQLite(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(db); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	service, err := NewService(db, &captureFixture{}, Options{
		PageSize: 50, ScanWorkers: 2, HotAge: 6 * time.Hour, WarmAge: 7 * 24 * time.Hour, Location: location,
		PrincipalOpenID: "ou_principal", SearchOverlap: 10 * time.Minute, ActivationContext: 2 * time.Hour,
		AutoRelatedP2PTopN: 30,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	observer := &recordingScanObserver{}
	if err := service.SetScanObserver(observer); err != nil {
		t.Fatalf("SetScanObserver() error = %v", err)
	}

	deliveredAt := time.Date(2026, 7, 28, 9, 0, 0, 0, location)
	service.now = func() time.Time { return deliveredAt }
	endedAt := time.Date(2026, 7, 27, 14, 1, 0, 0, location)
	input := ClueInput{
		Source: "feishu_meeting", ExternalID: "7667030332496007223",
		Title: "会议结束：公会基建Agent 日会", Content: "会议 ID：7667030332496007223",
		OccurredAt: endedAt,
	}
	first, err := service.AppendClue(context.Background(), input)
	if err != nil {
		t.Fatalf("AppendClue() error = %v", err)
	}
	if !first.Inserted || first.ChatID != "clue:feishu_meeting" ||
		first.MessageID != "clue:feishu_meeting:7667030332496007223" {
		t.Fatalf("AppendClue() = %#v", first)
	}
	if len(observer.results) != 1 || observer.results[0].ChatID != first.ChatID {
		t.Fatalf("observer results = %#v, want one notification for the clue channel", observer.results)
	}

	var channel domain.Group
	if err := db.Where("chat_id = ?", first.ChatID).First(&channel).Error; err != nil {
		t.Fatalf("load clue channel: %v", err)
	}
	if channel.ChatMode != ClueChatMode || !channel.RelatedGroup {
		t.Fatalf("clue channel = %#v, want chat_mode=clue and related_group=true", channel)
	}

	var message domain.Message
	if err := db.Where("message_id = ?", first.MessageID).First(&message).Error; err != nil {
		t.Fatalf("load clue message: %v", err)
	}
	// A clue about a past event must still land at the head of the stream, or
	// M3's watermark would skip it.
	if message.CreateTime != deliveredAt.UnixMilli() {
		t.Fatalf("clue create_time = %d, want delivery time %d", message.CreateTime, deliveredAt.UnixMilli())
	}
	if !strings.Contains(message.Content, endedAt.Format(time.RFC3339)) {
		t.Fatalf("clue content %q lost the occurred_at timestamp", message.Content)
	}
	// M3 drops bot/app senders and unrendered rows, so a clue must not look like one.
	if !message.RenderOK || message.SenderType == "bot" || message.SenderType == "app" {
		t.Fatalf("clue message %#v would be skipped by M3 extraction", message)
	}

	second, err := service.AppendClue(context.Background(), input)
	if err != nil {
		t.Fatalf("AppendClue() redelivery error = %v", err)
	}
	if second.Inserted {
		t.Fatalf("AppendClue() redelivery = %#v, want inserted=false", second)
	}
	if len(observer.results) != 1 {
		t.Fatalf("observer results = %d, want redelivery to stay silent", len(observer.results))
	}
}
