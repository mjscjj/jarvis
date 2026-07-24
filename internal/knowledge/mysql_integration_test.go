//go:build integration

package knowledge_test

import (
	"context"
	"os"
	"testing"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/knowledge"
	"jarvis/internal/store"
)

func TestRelationFactsMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_KNOWLEDGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("JARVIS_KNOWLEDGE_TEST_MYSQL_DSN is required")
	}
	db, err := store.OpenMySQL(context.Background(), config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: 60,
	})
	if err != nil {
		t.Fatalf("OpenMySQL() error = %v", err)
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

	created, err := service.Create(context.Background(), knowledge.CreateInput{
		EntityA:     knowledge.EntityRef{Type: knowledge.EntityProject, ID: project.ID},
		EntityB:     knowledge.EntityRef{Type: knowledge.EntityPerson, ID: person.ID},
		Description: "Owner 负责 Jarvis 项目。",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.EntityA.Label != "Owner" || created.EntityB.Label != "Jarvis" {
		t.Fatalf("canonical labeled entities = %#v / %#v", created.EntityA, created.EntityB)
	}

	upserted, err := service.Create(context.Background(), knowledge.CreateInput{
		EntityA:     knowledge.EntityRef{Type: knowledge.EntityPerson, ID: person.ID},
		EntityB:     knowledge.EntityRef{Type: knowledge.EntityProject, ID: project.ID},
		Description: "Owner 负责 Jarvis 项目的交付。",
	})
	if err != nil {
		t.Fatalf("upsert relation: %v", err)
	}
	if upserted.ID != created.ID || upserted.Description != "Owner 负责 Jarvis 项目的交付。" {
		t.Fatalf("upserted = %#v", upserted)
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

	updated, err := service.Update(context.Background(), knowledge.UpdateInput{
		FactID: created.ID, Description: "Owner 与 Jarvis 项目保持协作。",
	})
	if err != nil || updated.Description != "Owner 与 Jarvis 项目保持协作。" {
		t.Fatalf("Update() result=%#v error=%v", updated, err)
	}
	if err := service.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}
