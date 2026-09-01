package chat

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveTemporaryImageAcceptsPNGAndCleansUp(t *testing.T) {
	t.Parallel()
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := SaveTemporaryImage(bytes.NewReader(png))
	if err != nil {
		t.Fatalf("SaveTemporaryImage() error = %v", err)
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("path = %q, want .png suffix", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, png) {
		t.Fatal("saved PNG differs from upload")
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary image still exists after cleanup: %v", err)
	}
}

func TestSaveTemporaryImageAcceptsJPEG(t *testing.T) {
	t.Parallel()
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0xff, 0xd9}
	path, cleanup, err := SaveTemporaryImage(bytes.NewReader(jpeg))
	if err != nil {
		t.Fatalf("SaveTemporaryImage() error = %v", err)
	}
	defer cleanup()
	if filepath.Ext(path) != ".jpg" {
		t.Fatalf("path = %q, want .jpg suffix", path)
	}
}

func TestSaveTemporaryImageRejectsInvalidUploads(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content []byte
		wantErr string
	}{
		{name: "empty", wantErr: "empty"},
		{name: "unsupported", content: []byte("not an image"), wantErr: "PNG or JPEG"},
		{name: "too large", content: append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, MaxImageBytes)...), wantErr: "exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path, cleanup, err := SaveTemporaryImage(bytes.NewReader(test.content))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want %q", err, test.wantErr)
			}
			if path != "" || cleanup != nil {
				t.Fatalf("rejected upload returned path=%q cleanup=%v", path, cleanup != nil)
			}
		})
	}
}
