package api

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func NewUpdateFileHandler(root string) (app.HandlerFunc, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve update root: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("stat update root %q: %w", absolute, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("update root is not a directory: %s", absolute)
	}
	return func(_ context.Context, c *app.RequestContext) {
		filename := strings.TrimSpace(c.Param("filename"))
		if filename == "" || filename == "." || filename == ".." ||
			filepath.Base(filename) != filename {
			c.String(consts.StatusNotFound, "update file not found")
			return
		}
		path := filepath.Join(absolute, filename)
		fileInfo, err := os.Stat(path)
		if err != nil || !fileInfo.Mode().IsRegular() {
			c.String(consts.StatusNotFound, "update file not found")
			return
		}
		contentType := mime.TypeByExtension(filepath.Ext(filename))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		c.Response.Header.SetContentType(contentType)
		c.Response.Header.SetContentLength(int(fileInfo.Size()))
		if filename == "latest.json" {
			c.Response.Header.Set("Cache-Control", "no-cache")
		} else {
			c.Response.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		c.Response.SetStatusCode(consts.StatusOK)
		if string(c.Method()) == consts.MethodHead {
			return
		}
		file, err := os.Open(path)
		if err != nil {
			c.String(consts.StatusNotFound, "update file not found")
			return
		}
		c.SetBodyStream(file, int(fileInfo.Size()))
	}, nil
}
