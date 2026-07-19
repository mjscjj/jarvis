package capture

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"

	"gorm.io/gorm"
)

// TestCaptureMySQL validates discovery, no-backfill checkpoint initialization,
// thread flattening and idempotent message/resource persistence against MySQL.
// It requires a dedicated empty database in JARVIS_CAPTURE_TEST_MYSQL_DSN.
func TestCaptureMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_CAPTURE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JARVIS_CAPTURE_TEST_MYSQL_DSN is required for capture integration test")
	}
	db, err := store.OpenMySQL(context.Background(), config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: 60,
	})
	if err != nil {
		t.Fatalf("OpenMySQL() error = %v", err)
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
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	discoveredAt := time.Date(2026, 7, 19, 10, 0, 0, 0, location)
	service.now = func() time.Time { return discoveredAt }

	if err := service.DiscoverChats(context.Background()); err != nil {
		t.Fatalf("DiscoverChats() error = %v", err)
	}
	var checkpoint domain.Checkpoint
	if err := db.First(&checkpoint, "chat_id = ?", "oc_fixture").Error; err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if checkpoint.HighWaterCreateTime != discoveredAt.UnixMilli() || !checkpoint.BackfillDone {
		t.Fatalf("checkpoint = %#v", checkpoint)
	}

	if err := service.ScanChat(context.Background(), "oc_fixture"); err != nil {
		t.Fatalf("ScanChat() error = %v", err)
	}
	assertCount(t, db, &domain.Message{}, 2)
	assertCount(t, db, &domain.Resource{}, 2)
	if err := db.First(&checkpoint, "chat_id = ?", "oc_fixture").Error; err != nil {
		t.Fatalf("reload checkpoint: %v", err)
	}
	wantHW := time.Date(2026, 7, 19, 10, 2, 0, 0, location).UnixMilli()
	if checkpoint.HighWaterCreateTime != wantHW || checkpoint.LastScanStatus == nil || *checkpoint.LastScanStatus != "ok" {
		t.Fatalf("checkpoint after scan = %#v, want high water %d and ok", checkpoint, wantHW)
	}

	if err := service.ScanChat(context.Background(), "oc_fixture"); err != nil {
		t.Fatalf("second ScanChat() error = %v", err)
	}
	assertCount(t, db, &domain.Message{}, 2)
	assertCount(t, db, &domain.Resource{}, 2)
	var latest domain.ScanRecord
	if err := db.Order("id DESC").First(&latest).Error; err != nil {
		t.Fatalf("load latest scan: %v", err)
	}
	if latest.InsertedCount != 0 {
		t.Fatalf("second scan inserted_count = %d, want 0", latest.InsertedCount)
	}
}

type captureFixture struct{}

func (f *captureFixture) Run(_ context.Context, out any, args ...string) error {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "+chat-list"):
		response := out.(*ChatListResponse)
		response.OK = true
		response.Data.Chats = []CLIChat{{ChatID: "oc_fixture", ChatMode: "topic", Name: "fixture"}}
		return nil
	case strings.Contains(joined, "+chat-messages-list"):
		response := out.(*MessageListResponse)
		response.OK = true
		response.Data.Messages = []CLIMessage{{
			ChatID:      "oc_fixture",
			Content:     "evidence img_key:img_v3_fixture https://example.com/evidence",
			CreateTime:  "2026-07-19 10:01",
			MessageID:   "om_root",
			MessageType: "post",
			ThreadID:    "omt_fixture",
			Sender:      CLISender{ID: "cli_app", OpenBotID: "ou_bot", Name: "bot", SenderType: "app"},
			ThreadReplies: []CLIMessage{{
				ChatID:      "oc_fixture",
				Content:     "reply",
				CreateTime:  "2026-07-19 10:02",
				MessageID:   "om_reply",
				MessageType: "text",
				Sender:      CLISender{ID: "ou_user", Name: "user", SenderType: "user"},
			}},
		}}
		return nil
	default:
		return nil
	}
}

func assertCount(t *testing.T, db *gorm.DB, model any, want int64) {
	t.Helper()
	var got int64
	if err := db.Model(model).Count(&got).Error; err != nil {
		t.Fatalf("count %T: %v", model, err)
	}
	if got != want {
		t.Fatalf("count %T = %d, want %d", model, got, want)
	}
}
