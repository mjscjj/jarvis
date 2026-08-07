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

	"gorm.io/gorm"
)

// TestCaptureSQLite validates discovery, no-backfill checkpoint initialization,
// thread flattening and idempotent message/resource persistence against SQLite.
func TestCaptureSQLite(t *testing.T) {
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
	// 内部真人私聊发现即自动监听；服务号私聊、外部私聊与话题群不自动开。
	assertRelated(t, db, "oc_p2p_internal", true)
	assertRelated(t, db, "oc_p2p_bot", false)
	assertRelated(t, db, "oc_p2p_external", false)
	assertRelated(t, db, "oc_fixture", false)
	// 外部私聊不能被手动加入名单。
	if err := service.ReplaceRelatedGroups([]string{"oc_p2p_external"}); err == nil || !strings.Contains(err.Error(), "external p2p") {
		t.Fatalf("ReplaceRelatedGroups(external p2p) error = %v, want external p2p rejection", err)
	}
	if err := service.ScanChat(context.Background(), "oc_fixture"); err == nil || !strings.Contains(err.Error(), "is not a related group") {
		t.Fatalf("ScanChat() before selection error = %v", err)
	}
	if err := service.ReplaceRelatedGroups([]string{"oc_fixture"}); err != nil {
		t.Fatalf("ReplaceRelatedGroups() error = %v", err)
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
	if len(observer.results) != 1 || observer.results[0].ChatID != "oc_fixture" || observer.results[0].InsertedCount != 2 || len(observer.results[0].MessageIDs) != 2 {
		t.Fatalf("scan observer results = %#v", observer.results)
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
	if len(observer.results) != 1 {
		t.Fatalf("duplicate scan emitted observer result: %#v", observer.results)
	}

	// 存量私聊回填：先把内部私聊关掉模拟历史数据，再用 OpenInternalP2P 一次性开启。
	// 其 checkpoint 水位停在发现时刻(discoveredAt)，模拟"很久以后才纳入监听"，
	// 把 now 前移，验证打开监听后水位被抬到 now、只增量不回捞历史。
	if err := db.Model(&domain.Group{}).Where("chat_id = ?", "oc_p2p_internal").Update("related_group", false).Error; err != nil {
		t.Fatalf("reset internal p2p related flag: %v", err)
	}
	openedAt := discoveredAt.Add(30 * 24 * time.Hour)
	service.now = func() time.Time { return openedAt }
	opened, err := service.OpenInternalP2P()
	if err != nil {
		t.Fatalf("OpenInternalP2P() error = %v", err)
	}
	if opened != 1 {
		t.Fatalf("OpenInternalP2P() opened = %d, want 1", opened)
	}
	assertRelated(t, db, "oc_p2p_internal", true)
	assertRelated(t, db, "oc_p2p_external", false)
	// 水位被抬到纳入监听那一刻，后续 scan 只从此增量、不回捞历史。
	var p2pCheckpoint domain.Checkpoint
	if err := db.First(&p2pCheckpoint, "chat_id = ?", "oc_p2p_internal").Error; err != nil {
		t.Fatalf("load internal p2p checkpoint: %v", err)
	}
	if p2pCheckpoint.HighWaterCreateTime != openedAt.UnixMilli() {
		t.Fatalf("internal p2p high water = %d, want %d (opened moment, no backfill)", p2pCheckpoint.HighWaterCreateTime, openedAt.UnixMilli())
	}
}

type recordingScanObserver struct {
	results []ChatScanResult
}

func (o *recordingScanObserver) ChatScanned(_ context.Context, result ChatScanResult) error {
	o.results = append(o.results, result)
	return nil
}

func assertRelated(t *testing.T, db *gorm.DB, chatID string, want bool) {
	t.Helper()
	var group domain.Group
	if err := db.Select("related_group").Where("chat_id = ?", chatID).First(&group).Error; err != nil {
		t.Fatalf("load group %s: %v", chatID, err)
	}
	if group.RelatedGroup != want {
		t.Fatalf("group %s related_group = %t, want %t", chatID, group.RelatedGroup, want)
	}
}

type captureFixture struct{}

func (f *captureFixture) Run(_ context.Context, out any, args ...string) error {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "+chat-list"):
		response := out.(*ChatListResponse)
		response.OK = true
		response.Data.Chats = []CLIChat{
			{ChatID: "oc_fixture", ChatMode: "topic", Name: "fixture"},
			// 内部真人私聊：在 TopN 预算内应被自动纳入监听。
			{ChatID: "oc_p2p_internal", ChatMode: "p2p", Name: "内部同事", P2PTargetType: "user"},
			// 内部服务号私聊：target_type=bot，即便 external=false 也不自动开。
			{ChatID: "oc_p2p_bot", ChatMode: "p2p", Name: "审批助手", P2PTargetType: "bot"},
			// 外部私聊：不监听，related_group 必须为 0。
			{ChatID: "oc_p2p_external", ChatMode: "p2p", Name: "外部联系人", External: true, P2PTargetType: "user"},
		}
		return nil
	case strings.Contains(joined, "+messages-search"):
		response := out.(*MessageSearchListResponse)
		response.OK = true
		response.Data.Messages = []CLIMessage{
			{
				ChatID:      "oc_fixture",
				Content:     "evidence img_key:img_v3_fixture https://example.com/evidence",
				CreateTime:  "2026-07-19 10:01",
				MessageID:   "om_root",
				MessageType: "post",
				ThreadID:    "omt_fixture",
				Sender:      CLISender{ID: "cli_app", OpenBotID: "ou_bot", Name: "bot", SenderType: "app"},
			},
			{
				ChatID:      "oc_fixture",
				Content:     "reply",
				CreateTime:  "2026-07-19 10:02",
				MessageID:   "om_reply",
				MessageType: "text",
				ThreadID:    "omt_fixture",
				Sender:      CLISender{ID: "ou_user", Name: "user", SenderType: "user"},
			},
		}
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
