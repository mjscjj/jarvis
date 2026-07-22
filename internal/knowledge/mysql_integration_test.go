package knowledge_test

import (
	"context"
	"os"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/knowledge"
	"jarvis/internal/store"
)

func TestRelationFactsMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_KNOWLEDGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JARVIS_KNOWLEDGE_TEST_MYSQL_DSN is required")
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
	people := []domain.Person{
		{OpenID: "ou_owner", Name: "Owner", Role: "key", PriorityWeight: 1, IsActive: true},
		{OpenID: "ou_leader_1", Name: "Leader 1", Role: "leader", PriorityWeight: 1, IsActive: true},
		{OpenID: "ou_leader_2", Name: "Leader 2", Role: "leader", PriorityWeight: 1, IsActive: true},
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.Create(&people).Error; err != nil {
		t.Fatalf("create people: %v", err)
	}
	service, err := knowledge.NewService(db)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	from := time.Now().UTC().Add(-time.Hour)
	first, err := service.Create(context.Background(), knowledge.CreateInput{
		Subject:   knowledge.EntityRef{Type: knowledge.EntityPerson, ID: people[0].ID},
		Predicate: "reports_to", Object: &knowledge.EntityRef{Type: knowledge.EntityPerson, ID: people[1].ID},
		AssertionKind: "manual", ValidFrom: &from, SourceType: "test", SourceID: "first",
	})
	if err != nil {
		t.Fatalf("create first fact: %v", err)
	}
	secondFrom := from.Add(30 * time.Minute)
	second, err := service.Create(context.Background(), knowledge.CreateInput{
		Subject:   knowledge.EntityRef{Type: knowledge.EntityPerson, ID: people[0].ID},
		Predicate: "reports_to", Object: &knowledge.EntityRef{Type: knowledge.EntityPerson, ID: people[2].ID},
		AssertionKind: "manual", ValidFrom: &secondFrom, SourceType: "test", SourceID: "second",
	})
	if err != nil {
		t.Fatalf("create second fact: %v", err)
	}
	var storedFirst domain.RelationFact
	if err := db.First(&storedFirst, first.ID).Error; err != nil {
		t.Fatalf("load first fact: %v", err)
	}
	if storedFirst.Status != "superseded" || storedFirst.SupersededByID == nil || *storedFirst.SupersededByID != second.ID {
		t.Fatalf("superseded fact = %#v", storedFirst)
	}
	if _, err := service.Create(context.Background(), knowledge.CreateInput{
		Subject:   knowledge.EntityRef{Type: knowledge.EntityPerson, ID: people[0].ID},
		Predicate: "reports_to", Object: &knowledge.EntityRef{Type: knowledge.EntityPerson, ID: people[1].ID},
		AssertionKind: "manual", ValidFrom: &from, SourceType: "test", SourceID: "first",
	}); err != nil {
		t.Fatalf("retry superseded fact: %v", err)
	}
	subjectType := knowledge.EntityPerson
	subjectID := people[0].ID
	list, err := service.List(context.Background(), knowledge.FactFilter{
		SubjectType: &subjectType, SubjectID: &subjectID, Predicate: "reports_to",
		AsOf: secondFrom.Add(time.Minute), Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != second.ID {
		t.Fatalf("active facts = %#v", list)
	}
	retracted, err := service.Retract(context.Background(), knowledge.RetractInput{FactID: second.ID, By: "user", Reason: "incorrect"})
	if err != nil {
		t.Fatalf("Retract() error = %v", err)
	}
	if retracted.Status != "retracted" {
		t.Fatalf("retracted status = %q", retracted.Status)
	}
}
