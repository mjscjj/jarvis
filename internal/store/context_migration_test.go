package store

import (
	"context"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/config"
	"jarvis/internal/contextpack"
	"jarvis/internal/datatypes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextMigrationPreservesFrozenSourceAndIsRestartable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`CREATE TABLE todo(id integer primary key,title text,description text,context text,open_questions text,commitment_strength text,context_snapshot text,extraction_result text,content text)`,
		`CREATE TABLE task(id integer primary key,title text,background text,source_payload text)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	source := `{"new_semantics":{"precise_id":9007199254740993}}`
	capture := `{"messages":[{"message_id":"om_request","content":"review this"}],"conversation":[{"message_id":"om_link","content":"https://mr/456"},{"message_id":"om_request","content":"review this"}],"future_fact":{"keep":true}}`
	if err := db.Exec(`INSERT INTO todo VALUES(1,'review','brief','old context','[]','',?,?,NULL)`, capture, source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO task VALUES(2,'review',?,?)`, capture, source).Error; err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if err := migrateContextContent(db); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct{ table, column string }{{"todo", "content"}, {"task", "source_payload"}} {
		var raw string
		if err := db.Table(test.table).Select(test.column).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		if err := contextpack.Validate([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		got, err := contextpack.Source([]byte(raw))
		if err != nil || string(got) != source {
			t.Fatalf("source changed: %s %v", got, err)
		}
		if !strings.Contains(raw, "https://mr/456") || !strings.Contains(raw, "future_fact") {
			t.Fatalf("lost captured evidence: %s", raw)
		}
	}
	if db.Migrator().HasColumn("task", "background") || db.Migrator().HasColumn("todo", "context_snapshot") {
		t.Fatal("legacy carriers remain")
	}
}

// Run only against an explicitly prepared disposable SQLite backup.
func TestContextMigrationReplayCopy(t *testing.T) {
	path := os.Getenv("JARVIS_CONTEXT_REPLAY_DB")
	if path == "" {
		t.Skip("set JARVIS_CONTEXT_REPLAY_DB to a disposable .context-replay.db backup")
	}
	if !strings.HasSuffix(path, ".context-replay.db") {
		t.Fatal("replay path must end in .context-replay.db")
	}
	db, err := OpenSQLite(context.Background(), config.SQLiteConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer Close(db)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	for _, table := range []string{"todo", "task"} {
		column := "content"
		if table == "task" {
			column = "source_payload"
		}
		var rows []struct {
			ID      uint64
			Content datatypes.JSON
		}
		if err := db.Table(table).Select("id," + column + " AS content").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if err := contextpack.Validate(row.Content); err != nil {
				t.Fatalf("%s %d: %v", table, row.ID, err)
			}
		}
		t.Logf("validated %s: %d complete packets", table, len(rows))
	}
}

func TestMaterialMigrationKeepsOriginalsAndIsRestartable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "materials.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{`CREATE TABLE todo(id INTEGER PRIMARY KEY,content TEXT)`, `CREATE TABLE task(id INTEGER PRIMARY KEY,source_payload TEXT)`, `CREATE TABLE todo_event(id INTEGER PRIMARY KEY,snapshot TEXT)`} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	old := `{"brief":"简报","trigger":{"refs":["message:om_request"]},"scene":{"summary":"现场","refs":["conversation"]},"materials":{"source":{"source_message_ids":["om_request"],"precise":9007199254740993},"messages":["message:om_request"],"conversation":["message:om_link","message:om_request"],"message:om_link":{"message_id":"om_link","content":"原始链接"},"message:om_request":{"message_id":"om_request","content":"原始请求"},"project":{"value":1e400}}}`
	if err := db.Exec(`INSERT INTO todo VALUES(1,?)`, old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO task VALUES(1,?)`, old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO todo_event VALUES(1,?)`, `{"revision":1,"content":`+old+`}`).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrateContextContent(db); err != nil {
			t.Fatal(err)
		}
	}
	var flat string
	if err := db.Table("task").Select("source_payload").Scan(&flat).Error; err != nil {
		t.Fatal(err)
	}
	if err := contextpack.Validate([]byte(flat)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"原始链接", "原始请求", "9007199254740993", "1e400", "现场"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("lost %s", want)
		}
	}
	if strings.Count(flat, "原始请求") != 1 || strings.Contains(flat, `"materials"`) || strings.Contains(flat, `"refs"`) {
		t.Fatalf("old protocol survived: %s", flat)
	}
	source, err := contextpack.Source([]byte(flat))
	if err != nil || string(source) != `{"source_message_ids":["om_request"],"precise":9007199254740993}` {
		t.Fatalf("changed source: %s %v", source, err)
	}
	var event string
	if err := db.Table("todo_event").Select("snapshot").Scan(&event).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(event, `"capture"`) || !strings.Contains(event, `"revision":1`) {
		t.Fatal("event snapshot lost")
	}
}
