package morningbrief

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReaderListsCanonicalBriefsNewestFirst(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "data", "morning-brief")
	for _, date := range []string{"2026-08-03", "2026-08-04", "not-a-date"} {
		if err := os.MkdirAll(filepath.Join(archive, date), 0o755); err != nil {
			t.Fatal(err)
		}
		if date != "not-a-date" {
			path := filepath.Join(archive, date, "99-brief.md")
			if err := os.WriteFile(path, []byte("# brief "+date+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(archive, "README.md"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	reader, err := NewReader(root, time.FixedZone("CST", 8*60*60))
	if err != nil {
		t.Fatal(err)
	}
	briefs, err := reader.List(t.Context(), 1)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(briefs) != 1 || briefs[0].Date != "2026-08-04" || !strings.Contains(briefs[0].Content, "2026-08-04") {
		t.Fatalf("briefs = %#v", briefs)
	}
	if briefs[0].GeneratedAt.Location().String() != "CST" {
		t.Fatalf("generated location = %s", briefs[0].GeneratedAt.Location())
	}
}

func TestReaderMissingArchiveIsEmpty(t *testing.T) {
	reader, err := NewReader(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	briefs, err := reader.List(t.Context(), 14)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if briefs == nil || len(briefs) != 0 {
		t.Fatalf("briefs = %#v, want non-nil empty slice", briefs)
	}
}

func TestReaderFailsForIncompleteCanonicalArtifact(t *testing.T) {
	root := t.TempDir()
	dateDir := filepath.Join(root, "data", "morning-brief", "2026-08-04")
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(root, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.List(t.Context(), 14); err == nil || !strings.Contains(err.Error(), "99-brief.md") {
		t.Fatalf("missing artifact error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dateDir, "99-brief.md"), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.List(t.Context(), 14); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("empty artifact error = %v", err)
	}
}

func TestReaderRejectsInvalidConfigurationAndLimit(t *testing.T) {
	if _, err := NewReader("", time.UTC); err == nil {
		t.Fatal("NewReader should reject empty root")
	}
	if _, err := NewReader(t.TempDir(), nil); err == nil {
		t.Fatal("NewReader should reject nil location")
	}
	reader, err := NewReader(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.List(context.Background(), 0); err == nil {
		t.Fatal("List should reject a non-positive limit")
	}
}
