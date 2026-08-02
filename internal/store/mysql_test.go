package store

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemoveLegacyDecisionModuleMigratesAndIsIdempotent(t *testing.T) {
	db := openMigrationTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE todo (id INTEGER PRIMARY KEY, status TEXT NOT NULL)`,
		`CREATE TABLE task (
			id INTEGER PRIMARY KEY, todo_id INTEGER,
			confirmed_by TEXT, confirmed_at DATETIME, decision_payload JSON
		)`,
		`CREATE TABLE decision_audit (id INTEGER PRIMARY KEY, todo_id INTEGER)`,
		`INSERT INTO todo(id,status) VALUES (1,'auto'),(2,'dropped'),(3,'observing')`,
		`INSERT INTO task(id,todo_id,confirmed_by,confirmed_at,decision_payload)
		 VALUES (11,1,'m4_auto','2026-08-02 12:00:00','{}')`,
		`INSERT INTO decision_audit(id,todo_id) VALUES (21,1)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare legacy schema: %v", err)
		}
	}

	for run := 1; run <= 2; run++ {
		if err := removeLegacyDecisionModule(db); err != nil {
			t.Fatalf("removeLegacyDecisionModule() run %d: %v", run, err)
		}
	}

	var statuses []struct {
		ID     uint64
		Status string
	}
	if err := db.Table("todo").Order("id ASC").Scan(&statuses).Error; err != nil {
		t.Fatalf("load migrated Todos: %v", err)
	}
	want := []string{"materialized", "observing", "observing"}
	if len(statuses) != len(want) {
		t.Fatalf("Todo count = %d, want %d", len(statuses), len(want))
	}
	for i := range want {
		if statuses[i].Status != want[i] {
			t.Errorf("Todo id=%d status = %q, want %q", statuses[i].ID, statuses[i].Status, want[i])
		}
	}
	for _, column := range []string{"confirmed_by", "confirmed_at", "decision_payload"} {
		if db.Migrator().HasColumn("task", column) {
			t.Errorf("retired task.%s still exists", column)
		}
	}
	if db.Migrator().HasTable("decision_audit") {
		t.Error("retired decision_audit table still exists")
	}
}

func TestRemoveLegacyDecisionModuleRejectsAutoTodoWithoutTaskBeforeWrites(t *testing.T) {
	db := openMigrationTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE todo (id INTEGER PRIMARY KEY, status TEXT NOT NULL)`,
		`CREATE TABLE task (id INTEGER PRIMARY KEY, todo_id INTEGER, confirmed_by TEXT)`,
		`CREATE TABLE decision_audit (id INTEGER PRIMARY KEY, todo_id INTEGER)`,
		`INSERT INTO todo(id,status) VALUES (1,'auto'),(2,'dropped')`,
		`INSERT INTO decision_audit(id,todo_id) VALUES (21,1)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare legacy schema: %v", err)
		}
	}

	err := removeLegacyDecisionModule(db)
	if err == nil || !strings.Contains(err.Error(), "1 of 1 auto Todos have no Task") {
		t.Fatalf("removeLegacyDecisionModule() error = %v, want orphan rejection", err)
	}
	var statuses []string
	if err := db.Table("todo").Order("id ASC").Pluck("status", &statuses).Error; err != nil {
		t.Fatalf("load untouched Todos: %v", err)
	}
	if len(statuses) != 2 || statuses[0] != "auto" || statuses[1] != "dropped" {
		t.Fatalf("Todo statuses changed before validation completed: %v", statuses)
	}
	if !db.Migrator().HasColumn("task", "confirmed_by") {
		t.Error("task.confirmed_by was dropped before validation completed")
	}
	if !db.Migrator().HasTable("decision_audit") {
		t.Error("decision_audit was dropped before validation completed")
	}
}

func TestRemoveLegacyDecisionModuleAllowsFreshSchema(t *testing.T) {
	db := openMigrationTestDB(t)
	if err := removeLegacyDecisionModule(db); err != nil {
		t.Fatalf("removeLegacyDecisionModule() on fresh schema: %v", err)
	}
}

func openMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open migration test database: %v", err)
	}
	return db
}
