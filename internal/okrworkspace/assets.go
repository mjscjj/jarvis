package okrworkspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"jarvis/internal/okrworkspace/domain"
)

const defaultImageWidth = 320

// ImageStore persists pasted OKR screenshots outside the database. Database
// rows keep stable /okr-assets URLs, so images survive refreshes and work in
// other browsers instead of pointing at a tab-local blob: URL.
type ImageStore struct {
	root     string
	maxBytes int64
}

func NewImageStore(root string, maxBytes int64) (*ImageStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("create OKR image store: root is required")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("create OKR image store: max bytes must be greater than zero")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve OKR image directory %q: %w", root, err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("create OKR image directory %q: %w", absolute, err)
	}
	return &ImageStore{root: absolute, maxBytes: maxBytes}, nil
}

func (s *ImageStore) Root() string { return s.root }

func (s *ImageStore) Save(originalName string, source io.Reader) (domain.ImageRef, error) {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return domain.ImageRef{}, fmt.Errorf("save OKR image: store is not initialized")
	}
	if source == nil {
		return domain.ImageRef{}, fmt.Errorf("save OKR image: image is required")
	}
	data, err := io.ReadAll(io.LimitReader(source, s.maxBytes+1))
	if err != nil {
		return domain.ImageRef{}, fmt.Errorf("read OKR image: %w", err)
	}
	if len(data) == 0 {
		return domain.ImageRef{}, fmt.Errorf("save OKR image: image is empty")
	}
	if int64(len(data)) > s.maxBytes {
		return domain.ImageRef{}, fmt.Errorf("save OKR image: image exceeds %d bytes", s.maxBytes)
	}
	mimeType := http.DetectContentType(data)
	extension, ok := map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	}[mimeType]
	if !ok {
		return domain.ImageRef{}, fmt.Errorf("save OKR image: unsupported content type %s", mimeType)
	}

	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	filename := digest + extension
	target := filepath.Join(s.root, filename)
	if _, err := os.Stat(target); err != nil {
		if !os.IsNotExist(err) {
			return domain.ImageRef{}, fmt.Errorf("inspect OKR image %q: %w", target, err)
		}
		temp, err := os.CreateTemp(s.root, ".upload-*")
		if err != nil {
			return domain.ImageRef{}, fmt.Errorf("create temporary OKR image: %w", err)
		}
		tempName := temp.Name()
		defer os.Remove(tempName)
		if err := temp.Chmod(0o644); err != nil {
			_ = temp.Close()
			return domain.ImageRef{}, fmt.Errorf("set temporary OKR image mode: %w", err)
		}
		if _, err := temp.Write(data); err != nil {
			_ = temp.Close()
			return domain.ImageRef{}, fmt.Errorf("write temporary OKR image: %w", err)
		}
		if err := temp.Sync(); err != nil {
			_ = temp.Close()
			return domain.ImageRef{}, fmt.Errorf("sync temporary OKR image: %w", err)
		}
		if err := temp.Close(); err != nil {
			return domain.ImageRef{}, fmt.Errorf("close temporary OKR image: %w", err)
		}
		if err := os.Rename(tempName, target); err != nil {
			return domain.ImageRef{}, fmt.Errorf("publish OKR image: %w", err)
		}
	}

	name := cleanImageName(originalName)
	if name == "" {
		name = "截图" + extension
	}
	return domain.ImageRef{
		ID:    "img-" + digest[:16],
		Name:  name,
		URL:   "/okr-assets/" + filename,
		Width: defaultImageWidth,
	}, nil
}

func cleanImageName(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	characters := []rune(value)
	if len(characters) > 120 {
		value = string(characters[:120])
	}
	return value
}
