package fileconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicReplacesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new\n")); err != nil {
		t.Fatalf("WriteAtomic() error = %v", err)
	}
	content, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(content) != "new\n" {
		t.Fatalf("content = %q", content)
	}
}
