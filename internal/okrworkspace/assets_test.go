package okrworkspace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageStorePersistsStableContentAddressedImage(t *testing.T) {
	store, err := NewImageStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("GIF89a stable image payload")
	first, err := store.Save("../meeting\nshot.gif", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	second, err := store.Save("same.gif", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	if first.URL != second.URL || !strings.HasPrefix(first.URL, "/okr-assets/") {
		t.Fatalf("stable URLs = %q / %q", first.URL, second.URL)
	}
	if strings.ContainsAny(first.Name, "\r\n") {
		t.Fatalf("sanitized name = %q", first.Name)
	}
	filename := strings.TrimPrefix(first.URL, "/okr-assets/")
	got, err := os.ReadFile(filepath.Join(store.Root(), filename))
	if err != nil {
		t.Fatalf("read persisted image: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("persisted content = %q", got)
	}
}

func TestImageStoreRejectsUnsupportedAndOversizedFiles(t *testing.T) {
	store, err := NewImageStore(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save("note.txt", strings.NewReader("plain")); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("text Save() error = %v", err)
	}
	if _, err := store.Save("large.gif", bytes.NewReader([]byte("GIF89a payload too large"))); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized Save() error = %v", err)
	}
}
