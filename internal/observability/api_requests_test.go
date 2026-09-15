package observability

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/middlewares/server/recovery"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestAPIRequestLogRecordsRecoveredStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	logger, err := NewAPIRequestLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	h := server.New()
	// The journal must wrap recovery so its result line observes the final status.
	h.Use(logger.Middleware(), recovery.Recovery())
	h.GET("/api/panic", func(ctx context.Context, c *app.RequestContext) { panic("test failure") })
	response := ut.PerformRequest(h.Engine, "GET", "/api/panic", nil).Result()
	if response.StatusCode() != 500 {
		t.Fatalf("status=%d", response.StatusCode())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("got %d lines", len(lines))
	}
	var result apiRequestRecord
	if err := json.Unmarshal(lines[1], &result); err != nil {
		t.Fatal(err)
	}
	if result.Event != "result" || result.Status != response.StatusCode() {
		t.Fatalf("incorrect panic result: %+v", result)
	}
}

func TestAPIRequestLogFullBodiesAndRejectedRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log", "requests.jsonl")
	logger, err := NewAPIRequestLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	h := server.New()
	h.Use(logger.Middleware("/dev/api/"))
	h.POST("/api/test", func(ctx context.Context, c *app.RequestContext) {
		c.Data(400, "application/octet-stream", c.Request.Body())
	})
	h.GET("/dev/api/test", func(ctx context.Context, c *app.RequestContext) { c.Status(401) })
	for _, body := range [][]byte{[]byte(strings.Repeat("长正文\n", 50000)), {0xff, 0x00, 0xfe}} {
		response := ut.PerformRequest(h.Engine, "POST", "/api/test?q=1", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}, ut.Header{Key: "Cookie", Value: "session=do-not-log"}, ut.Header{Key: "Authorization", Value: "Bearer do-not-log"}).Result()
		if !bytes.Equal(response.Body(), body) {
			t.Fatal("middleware changed request body")
		}
	}
	ut.PerformRequest(h.Engine, "GET", "/dev/api/test", nil)
	ut.PerformRequest(h.Engine, "GET", "/api/unknown", nil)
	ut.PerformRequest(h.Engine, "GET", "/assets/app.js", nil)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("do-not-log")) {
		t.Fatal("logged credential header")
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 8 {
		t.Fatalf("got %d log lines", len(lines))
	}
	var records []apiRequestRecord
	for _, line := range lines {
		var record apiRequestRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if records[0].Body != strings.Repeat("长正文\n", 50000) || records[0].URI != "/api/test?q=1" || records[0].LogID != records[1].LogID || records[1].Status != 400 {
		t.Fatal("missing full payload/correlation/status")
	}
	binary, err := base64.StdEncoding.DecodeString(records[2].Body)
	if err != nil || !bytes.Equal(binary, []byte{0xff, 0, 0xfe}) || records[2].BodyEncoding != "base64" {
		t.Fatal("binary body cannot be recovered")
	}
	if records[5].Status != 401 || records[7].Status != 404 {
		t.Fatal("rejected request missing")
	}
}

func TestAPIRequestLogConcurrentRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	logger, err := NewAPIRequestLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			logger.append(context.Background(), apiRequestRecord{Event: "request", Body: strings.Repeat("正文", 10000)})
		})
	}
	wg.Wait()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 30 {
		t.Fatalf("got %d records", len(lines))
	}
	for _, line := range lines {
		if !json.Valid(line) {
			t.Fatal("concurrent records interleaved")
		}
	}
}
