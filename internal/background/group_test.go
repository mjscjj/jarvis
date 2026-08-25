package background

import (
	"context"
	"testing"

	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGroupListKeyOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:group-key-only?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.Project{}, &domain.Group{}, &domain.Message{}, &domain.Checkpoint{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	service, err := NewGroupBackgroundService(db, nil)
	if err != nil {
		t.Fatalf("NewGroupBackgroundService() error = %v", err)
	}
	keyName := "关键群"
	ordinaryName := "普通群"
	groups := []domain.Group{
		{ChatID: "oc_key", ChatMode: "group", Name: &keyName, RelatedGroup: true, IsKeyGroup: true, Tier: "hot"},
		{ChatID: "oc_ordinary", ChatMode: "group", Name: &ordinaryName, RelatedGroup: true, IsKeyGroup: false, Tier: "hot"},
	}
	if err := db.Create(&groups).Error; err != nil {
		t.Fatalf("create groups: %v", err)
	}
	result, err := service.List(context.Background(), GroupFilter{
		ListFilter: ListFilter{Page: 1, PageSize: 20}, KeyOnly: true,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ChatID != "oc_key" {
		t.Fatalf("List() = total=%d items=%+v", result.Total, result.Items)
	}
}

func TestGroupManualP2PMonitoringPinsTheConversation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:group-manual-p2p?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.Project{}, &domain.Group{}, &domain.Message{}, &domain.Checkpoint{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	service, err := NewGroupBackgroundService(db, nil)
	if err != nil {
		t.Fatalf("NewGroupBackgroundService() error = %v", err)
	}
	name := "手工固定私聊"
	group := domain.Group{
		ChatID: "oc_manual_p2p", ChatMode: "p2p", Name: &name, P2PTargetType: stringPointer("user"), Tier: "cold",
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatalf("create p2p: %v", err)
	}
	updated, err := service.UpdateBackground(context.Background(), group.ID, GroupBackgroundInput{
		RelatedGroup: true, IncludeInMemory: true,
	})
	if err != nil {
		t.Fatalf("UpdateBackground() error = %v", err)
	}
	if !updated.RelatedGroup || !updated.Pinned {
		t.Fatalf("manual p2p flags = related:%t pinned:%t, want true/true", updated.RelatedGroup, updated.Pinned)
	}
}

func stringPointer(value string) *string {
	return &value
}
