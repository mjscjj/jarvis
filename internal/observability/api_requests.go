package observability

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// APIRequestLogger appends the incoming payload before any handler can reject or
// transform it. A separate result line correlates the eventual HTTP status. No
// request headers containing credentials (Cookie, Authorization, etc.) are copied.
type APIRequestLogger struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
}

type apiRequestRecord struct {
	At           string `json:"at"`
	Event        string `json:"event"`
	LogID        string `json:"logid"`
	Method       string `json:"method,omitempty"`
	URI          string `json:"uri,omitempty"`
	ContentType  string `json:"content_type,omitempty"`
	UserAgent    string `json:"user_agent,omitempty"`
	RemoteAddr   string `json:"remote_addr,omitempty"`
	Body         string `json:"body,omitempty"`
	BodyEncoding string `json:"body_encoding,omitempty"`
	BodyBytes    int    `json:"body_bytes,omitempty"`
	Status       int    `json:"status,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
}

func NewAPIRequestLogger(path string) (*APIRequestLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(f)
	encoder.SetEscapeHTML(false)
	return &APIRequestLogger{file: f, encoder: encoder}, nil
}

func (l *APIRequestLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

func (l *APIRequestLogger) append(ctx context.Context, record apiRequestRecord) {
	record.At = time.Now().UTC().Format(time.RFC3339Nano)
	l.mu.Lock()
	err := l.encoder.Encode(record)
	l.mu.Unlock()
	if err != nil {
		// Disk errors are visible, but must not prevent an otherwise valid edit.
		hlog.CtxErrorf(ctx, "write API request log failed logid=%s: %v", record.LogID, err)
	}
}

// Middleware must be registered before recovery to observe its final status.
func (l *APIRequestLogger) Middleware(extraAPIPrefixes ...string) app.HandlerFunc {
	prefixes := append([]string{"/api/"}, extraAPIPrefixes...)
	return func(ctx context.Context, c *app.RequestContext) {
		isAPI := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(string(c.Path()), prefix) {
				isAPI = true
				break
			}
		}
		if !isAPI {
			c.Next(ctx)
			return
		}
		ctx = FromRequestContext(ctx, c)
		started := time.Now()
		body := c.Request.Body()
		payload, encoding := string(body), "utf-8"
		if !utf8.Valid(body) {
			payload, encoding = base64.StdEncoding.EncodeToString(body), "base64"
		}
		l.append(ctx, apiRequestRecord{
			Event: "request", LogID: LogID(ctx), Method: string(c.Method()),
			URI: string(c.Request.URI().RequestURI()), ContentType: string(c.Request.Header.ContentType()),
			UserAgent: string(c.Request.Header.UserAgent()), RemoteAddr: c.RemoteAddr().String(),
			Body: payload, BodyEncoding: encoding, BodyBytes: len(body),
		})
		defer func() {
			l.append(ctx, apiRequestRecord{Event: "result", LogID: LogID(ctx), Status: c.Response.StatusCode(), DurationMS: time.Since(started).Milliseconds()})
		}()
		c.Next(ctx)
	}
}
