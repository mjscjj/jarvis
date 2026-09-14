package api

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/protocol/http1/resp"
)

// sseWriter is the small server-side subset Jarvis needs. The internal Hertz
// fork does not expose the newer OSS protocol/sse package, so this writes SSE
// frames on top of its public chunked response writer.
type sseWriter struct {
	writer network.ExtWriter
	mu     sync.Mutex
}

func newSSEWriter(c *app.RequestContext) *sseWriter {
	c.Response.Header.Set("Cache-Control", "no-cache")
	c.Response.Header.SetContentType("text/event-stream; charset=utf-8")
	writer := c.Response.GetHijackWriter()
	if writer == nil {
		writer = resp.NewChunkedBodyWriter(&c.Response, c.GetWriter())
		c.Response.HijackWriter(writer)
	}
	return &sseWriter{writer: writer}
}

func (w *sseWriter) WriteEvent(eventType string, data []byte) error {
	if strings.ContainsAny(eventType, "\r\n") {
		return fmt.Errorf("SSE event type contains CR or LF")
	}
	var frame strings.Builder
	if eventType != "" {
		frame.WriteString("event: ")
		frame.WriteString(eventType)
		frame.WriteByte('\n')
	}
	if len(data) > 0 {
		normalized := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(string(data))
		for _, line := range strings.Split(normalized, "\n") {
			frame.WriteString("data: ")
			frame.WriteString(line)
			frame.WriteByte('\n')
		}
	}
	frame.WriteByte('\n')

	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.writer.Write([]byte(frame.String())); err != nil {
		return err
	}
	return w.writer.Flush()
}

func (w *sseWriter) WriteComment(comment string) error {
	if strings.ContainsAny(comment, "\r\n") {
		return fmt.Errorf("SSE comment contains CR or LF")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := fmt.Fprintf(w.writer, ": %s\n\n", comment); err != nil {
		return err
	}
	return w.writer.Flush()
}

func startSSEHeartbeat(write func() error, interval time.Duration) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(done)
		for {
			select {
			case <-ticker.C:
				if err := write(); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()
	return sync.OnceFunc(func() {
		close(stop)
		<-done
	})
}

func (w *sseWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Finalize()
}
