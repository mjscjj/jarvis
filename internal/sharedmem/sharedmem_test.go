package sharedmem

import (
	"context"
	"os"
	"strings"
	"testing"

	"jarvis/internal/config"
	"jarvis/internal/store"
)

func TestRenderBlockEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\t "} {
		if got := RenderBlock(in); got != "" {
			t.Fatalf("RenderBlock(%q) = %q, want empty", in, got)
		}
	}
}

func TestRenderBlockNonEmpty(t *testing.T) {
	block := RenderBlock("  线上库密码是 hunter2\n别直连生产  ")
	for _, want := range []string{
		"BEGIN_SHARED_MEMORY", "END_SHARED_MEMORY", "可信",
		"线上库密码是 hunter2", "别直连生产",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("RenderBlock() missing %q:\n%s", want, block)
		}
	}
	// 首尾空白被 trim：内容行不带前导两个空格。
	if strings.Contains(block, "  线上库密码") {
		t.Fatalf("RenderBlock() did not trim leading whitespace:\n%s", block)
	}
}

// TestAppendNote 覆盖纯拼接逻辑：空 content 直接是这条、非空用 "\n" 连接、note 被
// trim。不依赖 DB。
func TestAppendNote(t *testing.T) {
	cases := []struct {
		name    string
		content string
		note    string
		want    string
	}{
		{"empty content", "", "  第一条  ", "第一条"},
		{"blank content", "  \n\t ", "第一条", "第一条"},
		{"append to existing", "已有记忆", "  新一条  ", "已有记忆\n新一条"},
		{"multiline existing", "第一行\n第二行", "第三行", "第一行\n第二行\n第三行"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := appendNote(c.content, c.note); got != c.want {
				t.Fatalf("appendNote(%q, %q) = %q, want %q", c.content, c.note, got, c.want)
			}
		})
	}
}

func TestNewSharedMemoryServiceNilDB(t *testing.T) {
	if _, err := NewSharedMemoryService(nil); err == nil {
		t.Fatal("NewSharedMemoryService(nil) must fail fast")
	}
}

// TestSharedMemoryServiceMySQL 覆盖 Get 无行返回空视图、Upsert 建行/更新、Text
// trim/空值。opt-in，需真实 MySQL：
//
//	JARVIS_SHAREDMEM_TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/jarvis_sm_test?parseTime=true' \
//	  go test ./internal/sharedmem -run TestSharedMemoryServiceMySQL
func TestSharedMemoryServiceMySQL(t *testing.T) {
	dsn := os.Getenv("JARVIS_SHAREDMEM_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("JARVIS_SHAREDMEM_TEST_MYSQL_DSN is required for shared memory integration test")
	}
	db, err := store.OpenMySQL(context.Background(), config.MySQLConfig{
		DSN: dsn, MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: 60,
	})
	if err != nil {
		t.Fatalf("OpenMySQL() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(db); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	// 清掉可能残留的单例行，保证幂等重跑。
	if err := db.Exec("DELETE FROM shared_memory WHERE singleton_key = ?", singletonKey).Error; err != nil {
		t.Fatalf("clean shared_memory: %v", err)
	}

	ctx := context.Background()
	svc, err := NewSharedMemoryService(db)
	if err != nil {
		t.Fatalf("NewSharedMemoryService() error = %v", err)
	}

	// 无行：空视图、空文本，不报错。
	view, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get() empty error = %v", err)
	}
	if view.Saved || view.Content != "" {
		t.Fatalf("empty Get() = %#v", view)
	}
	text, err := svc.Text(ctx)
	if err != nil || text != "" {
		t.Fatalf("empty Text() = %q err=%v", text, err)
	}

	// 无行 -> Create。
	if _, err := svc.Upsert(ctx, "  第一版记忆  ", "agent"); err != nil {
		t.Fatalf("Upsert() create error = %v", err)
	}
	view, err = svc.Get(ctx)
	if err != nil || !view.Saved || view.Content != "  第一版记忆  " || view.UpdatedBy != "agent" {
		t.Fatalf("after create Get() = %#v err=%v", view, err)
	}
	// Text 对内容 trim。
	if text, err := svc.Text(ctx); err != nil || text != "第一版记忆" {
		t.Fatalf("Text() = %q err=%v, want trimmed", text, err)
	}

	// 有行 -> Update（仍是同一行，不新增）。
	if _, err := svc.Upsert(ctx, "第二版记忆", "human"); err != nil {
		t.Fatalf("Upsert() update error = %v", err)
	}
	view, err = svc.Get(ctx)
	if err != nil || view.Content != "第二版记忆" || view.UpdatedBy != "human" {
		t.Fatalf("after update Get() = %#v err=%v", view, err)
	}
	var count int64
	if err := db.Table("shared_memory").Where("singleton_key = ?", singletonKey).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("singleton row count = %d, want 1", count)
	}

	// Append：在既有内容后追加一条（"\n" 连接、note 被 trim），仍是同一行。
	if _, err := svc.Append(ctx, "  第三条踩坑  ", "agent"); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	view, err = svc.Get(ctx)
	if err != nil || view.Content != "第二版记忆\n第三条踩坑" || view.UpdatedBy != "agent" {
		t.Fatalf("after append Get() = %#v err=%v", view, err)
	}

	// 空白 note fail-fast，且不改动已有内容。
	if _, err := svc.Append(ctx, "   ", "agent"); err == nil {
		t.Fatal("Append() with blank note must fail fast")
	}
	if view, err := svc.Get(ctx); err != nil || view.Content != "第二版记忆\n第三条踩坑" {
		t.Fatalf("blank append must not mutate: %#v err=%v", view, err)
	}
}
