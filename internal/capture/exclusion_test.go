package capture

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"
)

func TestCaptureExclusionStopsWritesAndRestoresFromCurrentTime(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	service := newDiscoverTestService(t, db, &discoverRotationFixture{}, 2)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, service.opts.Location)
	service.now = func() time.Time { return now }
	group := domain.Group{ChatID: "oc_private", ChatMode: "group", RelatedGroup: true, Pinned: true, Tier: "hot"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&domain.Checkpoint{
		ChatID: group.ChatID, HighWaterCreateTime: now.Add(-time.Hour).UnixMilli(),
		BackfillDone: true, BackfillSince: now.Add(-time.Hour).UnixMilli(),
	}).Error; err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}

	if updated, err := service.SetCaptureExclusion(context.Background(), []uint64{group.ID}, true); err != nil || updated != 1 {
		t.Fatalf("exclude updated=%d error=%v", updated, err)
	}
	if err := db.Where("id = ?", group.ID).Take(&group).Error; err != nil {
		t.Fatalf("reload excluded group: %v", err)
	}
	if !group.CaptureExcluded || group.RelatedGroup || group.Pinned {
		t.Fatalf("excluded flags = %+v", group)
	}
	if err := service.ValidateScanChat(context.Background(), group.ChatID); !errors.Is(err, ErrCaptureExcluded) {
		t.Fatalf("ValidateScanChat() error = %v, want ErrCaptureExcluded", err)
	}
	if err := service.ScanChat(context.Background(), group.ChatID); !errors.Is(err, ErrCaptureExcluded) {
		t.Fatalf("ScanChat() error = %v, want ErrCaptureExcluded", err)
	}
	if _, _, _, _, err := service.persistMessagePage(&group, []CLIMessage{
		topicMessage("om_blocked", "", "blocked", "2026-09-13 12:01"),
	}, now.Add(-time.Hour).UnixMilli(), nil); !errors.Is(err, ErrCaptureExcluded) {
		t.Fatalf("persistMessagePage() error = %v, want ErrCaptureExcluded", err)
	}

	if updated, err := service.SetCaptureExclusion(context.Background(), []uint64{group.ID}, false); err != nil || updated != 1 {
		t.Fatalf("restore updated=%d error=%v", updated, err)
	}
	var checkpoint domain.Checkpoint
	if err := db.Where("chat_id = ?", group.ChatID).Take(&checkpoint).Error; err != nil {
		t.Fatalf("reload checkpoint: %v", err)
	}
	if checkpoint.HighWaterCreateTime != now.UnixMilli() || checkpoint.CaptureFloor != now.UnixMilli() {
		t.Fatalf("restored checkpoint = %+v, want floor %d", checkpoint, now.UnixMilli())
	}
	if err := db.Where("id = ?", group.ID).Take(&group).Error; err != nil {
		t.Fatalf("reload restored group: %v", err)
	}
	if group.CaptureExcluded || !group.RelatedGroup || !group.Pinned {
		t.Fatalf("restored flags = %+v", group)
	}
	inserted, ids, _, _, err := service.persistMessagePage(&group, []CLIMessage{
		topicMessage("om_before_floor", "", "old", "2026-09-13 11:59"),
		topicMessage("om_after_floor", "", "new", "2026-09-13 12:01"),
	}, checkpoint.HighWaterCreateTime, nil)
	if err != nil {
		t.Fatalf("persist after restore: %v", err)
	}
	if inserted != 1 || len(ids) != 1 || ids[0] != "om_after_floor" {
		t.Fatalf("persist after restore inserted=%d ids=%v", inserted, ids)
	}
}

func TestAutomaticP2PDisabledPreservesPinnedAndAdvancesOthers(t *testing.T) {
	db := newDiscoverTestDB(t)
	service := newDiscoverTestService(t, db, &discoverRotationFixture{}, 0)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, service.opts.Location)
	service.now = func() time.Time { return now }
	groups := []domain.Group{
		{ChatID: "oc_auto", ChatMode: "p2p", P2PTargetType: captureStringPointer("user"), RelatedGroup: true, Tier: "hot"},
		{ChatID: "oc_pinned", ChatMode: "p2p", P2PTargetType: captureStringPointer("user"), RelatedGroup: true, Pinned: true, Tier: "hot"},
	}
	if err := db.Create(&groups).Error; err != nil {
		t.Fatalf("create p2p groups: %v", err)
	}
	for _, group := range groups {
		if err := db.Create(&domain.Checkpoint{
			ChatID: group.ChatID, HighWaterCreateTime: now.Add(-time.Hour).UnixMilli(),
			BackfillDone: true, BackfillSince: now.Add(-time.Hour).UnixMilli(),
		}).Error; err != nil {
			t.Fatalf("create checkpoint: %v", err)
		}
	}
	if err := service.disableAutomaticP2P(context.Background()); err != nil {
		t.Fatalf("disableAutomaticP2P() error = %v", err)
	}
	assertDiscoverRelated(t, db, "oc_auto", false)
	assertDiscoverRelated(t, db, "oc_pinned", true)
	var checkpoint domain.Checkpoint
	if err := db.Where("chat_id = ?", "oc_auto").Take(&checkpoint).Error; err != nil {
		t.Fatalf("load automatic checkpoint: %v", err)
	}
	if checkpoint.HighWaterCreateTime != now.UnixMilli() || checkpoint.CaptureFloor != now.UnixMilli() {
		t.Fatalf("automatic checkpoint = %+v", checkpoint)
	}
}

func TestRestoringExternalP2PDoesNotBypassMonitoringPolicy(t *testing.T) {
	db := newDiscoverTestDB(t)
	service := newDiscoverTestService(t, db, &discoverRotationFixture{}, 2)
	group := domain.Group{
		ChatID: "oc_external", ChatMode: "p2p", External: true,
		CaptureExcluded: true, Tier: "cold",
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create external p2p: %v", err)
	}
	if updated, err := service.SetCaptureExclusion(context.Background(), []uint64{group.ID}, false); err != nil || updated != 1 {
		t.Fatalf("restore updated=%d error=%v", updated, err)
	}
	if err := db.Where("id = ?", group.ID).Take(&group).Error; err != nil {
		t.Fatalf("reload external p2p: %v", err)
	}
	if group.CaptureExcluded || group.RelatedGroup || group.Pinned {
		t.Fatalf("restored external p2p flags = %+v", group)
	}
}

func captureStringPointer(value string) *string { return &value }
