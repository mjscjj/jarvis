//go:build integration

package knowledge_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/knowledge"
	"jarvis/internal/store"
)

func TestRelationFactsSQLite(t *testing.T) {
	db, err := store.OpenSQLite(context.Background(), config.SQLiteConfig{
		Path: filepath.Join(t.TempDir(), "jarvis.db"),
	})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	project := domain.Project{Name: "Jarvis", Role: "owner", Status: "active", Priority: 1}
	person := domain.Person{OpenID: "ou_owner", Name: "Owner", Role: "key", PriorityWeight: 1, IsActive: true}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.Create(&person).Error; err != nil {
		t.Fatalf("create person: %v", err)
	}
	service, err := knowledge.NewService(db)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	validFrom := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	created, err := service.Create(context.Background(), knowledge.CreateInput{
		EntityA:     knowledge.EntityRef{Type: knowledge.EntityProject, ID: project.ID},
		EntityB:     knowledge.EntityRef{Type: knowledge.EntityPerson, ID: person.ID},
		Description: "Owner 负责 Jarvis 项目。",
		ValidFrom:   &validFrom,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.EntityA.Label != "Owner" || created.EntityB.Label != "Jarvis" {
		t.Fatalf("canonical labeled entities = %#v / %#v", created.EntityA, created.EntityB)
	}
	if created.ValidFrom == nil || !created.ValidFrom.Equal(validFrom) {
		t.Fatalf("created valid_from = %v, want %v", created.ValidFrom, validFrom)
	}
	if created.ValidUntil != nil {
		t.Fatalf("created valid_until = %v, want nil for a current relationship", created.ValidUntil)
	}

	// Ending a relationship is an upsert that sets valid_until; the row stays so
	// "used to own this" remains queryable.
	validUntil := time.Date(2026, 7, 15, 18, 0, 0, 0, time.UTC)
	upserted, err := service.Create(context.Background(), knowledge.CreateInput{
		EntityA:     knowledge.EntityRef{Type: knowledge.EntityPerson, ID: person.ID},
		EntityB:     knowledge.EntityRef{Type: knowledge.EntityProject, ID: project.ID},
		Description: "Owner 负责 Jarvis 项目的交付。",
		ValidFrom:   &validFrom,
		ValidUntil:  &validUntil,
	})
	if err != nil {
		t.Fatalf("upsert relation: %v", err)
	}
	if upserted.ID != created.ID || upserted.Description != "Owner 负责 Jarvis 项目的交付。" {
		t.Fatalf("upserted = %#v", upserted)
	}
	if upserted.ValidUntil == nil || !upserted.ValidUntil.Equal(validUntil) {
		t.Fatalf("upserted valid_until = %v, want %v", upserted.ValidUntil, validUntil)
	}

	entityType := knowledge.EntityProject
	entityID := project.ID
	list, err := service.List(context.Background(), knowledge.FactFilter{
		EntityType: &entityType, EntityID: &entityID, Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("facts = %#v", list)
	}

	// Update replaces the whole editable payload, so omitting both bounds clears
	// them rather than silently keeping the stored period.
	updated, err := service.Update(context.Background(), knowledge.UpdateInput{
		FactID: created.ID, Description: "Owner 与 Jarvis 项目保持协作。",
	})
	if err != nil || updated.Description != "Owner 与 Jarvis 项目保持协作。" {
		t.Fatalf("Update() result=%#v error=%v", updated, err)
	}
	if updated.ValidFrom != nil || updated.ValidUntil != nil {
		t.Fatalf("updated period = %v / %v, want both cleared", updated.ValidFrom, updated.ValidUntil)
	}
	if err := service.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}
