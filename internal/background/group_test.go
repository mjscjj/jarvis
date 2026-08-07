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
