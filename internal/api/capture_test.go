package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/capture"
	"jarvis/internal/domain"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type captureRunnerStub struct{}

func (captureRunnerStub) Run(context.Context, any, ...string) error { return nil }

func TestScanChatManuallyRejectsP2PWhenScanningDisabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "capture.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Checkpoint{}, &domain.ScanRecord{}); err != nil {
		t.Fatalf("migrate capture schema: %v", err)
	}
	if err := db.Create(&domain.Group{
		ChatID: "oc_direct", ChatMode: "p2p", RelatedGroup: true, Tier: "hot",
	}).Error; err != nil {
		t.Fatalf("create p2p group: %v", err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	service, err := capture.NewService(db, captureRunnerStub{}, capture.Options{
		PageSize:            50,
		ScanWorkers:         1,
		HotAge:              6 * time.Hour,
		WarmAge:             7 * 24 * time.Hour,
		Location:            location,
		PrincipalOpenID:     "ou_principal",
		SearchOverlap:       10 * time.Minute,
		ActivationContext:   2 * time.Hour,
		P2PActivationWindow: 15 * time.Minute,
		P2PScanEnabled:      false,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	h := server.New()
	h.POST("/api/debug/capture/scan-chat", ScanChatManually(service))
	body := []byte(`{"chat_id":"oc_direct"}`)

	response := ut.PerformRequest(
		h.Engine, "POST", "/api/debug/capture/scan-chat",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
	).Result()
	if response.StatusCode() != consts.StatusForbidden {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != 40330 || payload.Msg != capture.ErrP2PScanDisabled.Error() {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestUpdateCaptureExclusion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "capture-exclusion.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.Group{}, &domain.Checkpoint{}, &domain.ScanRecord{}); err != nil {
		t.Fatalf("migrate capture schema: %v", err)
	}
	group := domain.Group{ChatID: "oc_group", ChatMode: "group", RelatedGroup: true, Pinned: true, Tier: "hot"}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := db.Create(&domain.Checkpoint{ChatID: group.ChatID, HighWaterCreateTime: 1, BackfillDone: true, BackfillSince: 1}).Error; err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	service, err := capture.NewService(db, captureRunnerStub{}, capture.Options{
		PageSize: 50, ScanWorkers: 1, HotAge: 6 * time.Hour, WarmAge: 7 * 24 * time.Hour,
		Location: location, PrincipalOpenID: "ou_principal", SearchOverlap: 10 * time.Minute,
		ActivationContext: 2 * time.Hour, P2PActivationWindow: 15 * time.Minute, P2PScanEnabled: true,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	h := server.New()
	h.PUT("/api/groups/capture-exclusion", UpdateCaptureExclusion(service))
	body := []byte(fmt.Sprintf(`{"group_ids":[%d],"excluded":true}`, group.ID))
	response := ut.PerformRequest(h.Engine, "PUT", "/api/groups/capture-exclusion", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if err := db.Where("id = ?", group.ID).Take(&group).Error; err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if !group.CaptureExcluded || group.RelatedGroup || group.Pinned {
		t.Fatalf("excluded group = %+v", group)
	}
}
