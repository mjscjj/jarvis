package api

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

type recordingExtWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	flushes   int
	finalized bool
}

func (w *recordingExtWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(data)
}

func (w *recordingExtWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushes++
	return nil
}

func (w *recordingExtWriter) Finalize() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finalized = true
	return nil
}

func (w *recordingExtWriter) snapshot() (string, int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String(), w.flushes, w.finalized
}

func TestSSEWriterWritesAndFlushesComment(t *testing.T) {
	c := app.NewContext(0)
	recording := &recordingExtWriter{}
	c.Response.HijackWriter(recording)
	w := newSSEWriter(c)

	if err := w.WriteComment("keepalive"); err != nil {
		t.Fatal(err)
	}
	if body, flushes, _ := recording.snapshot(); body != ": keepalive\n\n" || flushes != 1 {
		t.Fatalf("comment body=%q flushes=%d", body, flushes)
	}
	if got := string(c.Response.Header.ContentType()); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := string(c.Response.Header.Peek("Cache-Control")); got != "no-cache" {
		t.Fatalf("cache control = %q", got)
	}
	if err := w.WriteComment("bad\ncomment"); err == nil {
		t.Fatal("multiline comment was accepted")
	}
}

type heartbeatRecorder struct {
	mu       sync.Mutex
	writes   int
	wrote    chan struct{}
	failNext bool
}

func (w *heartbeatRecorder) WriteComment(comment string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if comment != "keepalive" {
		return errors.New("unexpected heartbeat comment")
	}
	if w.failNext {
		return errors.New("connection closed")
	}
	w.writes++
	select {
	case w.wrote <- struct{}{}:
	default:
	}
	return nil
}

func (w *heartbeatRecorder) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

func TestSSEHeartbeatStopsBeforeTerminalWrite(t *testing.T) {
	recording := &heartbeatRecorder{wrote: make(chan struct{}, 1)}
	heartbeat := startSSEHeartbeat(recording, time.Millisecond)
	select {
	case <-recording.wrote:
	case <-time.After(time.Second):
		t.Fatal("heartbeat was not written")
	}
	heartbeat.Stop()
	heartbeat.Stop()
	writes := recording.count()
	time.Sleep(5 * time.Millisecond)
	if got := recording.count(); got != writes {
		t.Fatalf("heartbeat continued after stop: before=%d after=%d", writes, got)
	}
}

func TestSSEHeartbeatExitsWhenConnectionCloses(t *testing.T) {
	recording := &heartbeatRecorder{wrote: make(chan struct{}, 1), failNext: true}
	heartbeat := startSSEHeartbeat(recording, time.Millisecond)
	select {
	case <-heartbeat.done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not stop after write failure")
	}
	heartbeat.Stop()
}
