package chat

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

const (
	// MaxImageBytes is the hard upload boundary for one chat screenshot.
	MaxImageBytes = 10 << 20
	// MaxRequestBodyBytes leaves room for multipart framing, the message and page context.
	MaxRequestBodyBytes = 12 << 20
)

// SaveTemporaryImage validates one PNG/JPEG by content and stores it only for
// the lifetime of the current Codex turn. The caller must invoke cleanup.
func SaveTemporaryImage(source io.Reader) (path string, cleanup func(), err error) {
	content, err := io.ReadAll(io.LimitReader(source, MaxImageBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("read chat image: %w", err)
	}
	if len(content) == 0 {
		return "", nil, fmt.Errorf("chat image is empty")
	}
	if len(content) > MaxImageBytes {
		return "", nil, fmt.Errorf("chat image exceeds %d bytes", MaxImageBytes)
	}

	var suffix string
	switch contentType := http.DetectContentType(content); contentType {
	case "image/png":
		suffix = ".png"
	case "image/jpeg":
		suffix = ".jpg"
	default:
		return "", nil, fmt.Errorf("chat image must be PNG or JPEG, got %q", contentType)
	}

	file, err := os.CreateTemp("", "jarvis-chat-image-*"+suffix)
	if err != nil {
		return "", nil, fmt.Errorf("create temporary chat image: %w", err)
	}
	path = file.Name()
	remove := func() { _ = os.Remove(path) }
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		remove()
		return "", nil, fmt.Errorf("secure temporary chat image: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		remove()
		return "", nil, fmt.Errorf("write temporary chat image: %w", err)
	}
	if err := file.Close(); err != nil {
		remove()
		return "", nil, fmt.Errorf("close temporary chat image: %w", err)
	}
	return path, remove, nil
}
