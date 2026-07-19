package background

import (
	"context"
	"os"
	"testing"

	"jarvis/internal/config"
	"jarvis/internal/store"
)

// TestSeedIdempotentMySQL verifies the one-shot seed creates the inferred
// project/task backgrounds once and creates nothing on a re-run. It is opt-in
// against a dedicated test database (the real group links are skipped there
// because the seed group names only exist in the owner's live DB):
//
//	JARVIS_BACKGROUND_TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/jarvis_bg_test?parseTime=true' \
//	  go test ./internal/background -run TestSeedIdempotentMySQL
func TestSeedIdempotentMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_BACKGROUND_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JARVIS_BACKGROUND_TEST_MYSQL_DSN is required for seed integration test")
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

	ctx := context.Background()
	first, err := Seed(ctx, db)
	if err != nil {
		t.Fatalf("Seed() first run error = %v", err)
	}
	if first.ProjectsCreated != len(seedProjects) {
		t.Fatalf("first run ProjectsCreated = %d, want %d", first.ProjectsCreated, len(seedProjects))
	}
	if first.TasksCreated != len(seedTasks) {
		t.Fatalf("first run TasksCreated = %d, want %d", first.TasksCreated, len(seedTasks))
	}

	second, err := Seed(ctx, db)
	if err != nil {
		t.Fatalf("Seed() second run error = %v", err)
	}
	if second.ProjectsCreated != 0 || second.TasksCreated != 0 {
		t.Fatalf("second run created rows: projects=%d tasks=%d, want 0/0 (not idempotent)", second.ProjectsCreated, second.TasksCreated)
	}
	if second.ProjectsSkipped != len(seedProjects) || second.TasksSkipped != len(seedTasks) {
		t.Fatalf("second run skipped: projects=%d tasks=%d, want %d/%d", second.ProjectsSkipped, second.TasksSkipped, len(seedProjects), len(seedTasks))
	}
}
