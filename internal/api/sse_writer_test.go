package api

import (
	"bytes"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

type recordingExtWriter struct {
	buf       bytes.Buffer
	flushes   int
	finalized bool
}

func (w *recordingExtWriter) Write(data []byte) (int, error) {
	return w.buf.Write(data)
}

func (w *recordingExtWriter) Flush() error {
	w.flushes++
	return nil
}

func (w *recordingExtWriter) Finalize() error {
	w.finalized = true
	return nil
}

func (w *recordingExtWriter) snapshot() (string, int, bool) {
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

func TestSSEHeartbeatStopsBeforeTerminalWrite(t *testing.T) {
	wrote := make(chan struct{}, 1)
	var writes atomic.Int32
	stop := startSSEHeartbeat(func() error {
		writes.Add(1)
		select {
		case wrote <- struct{}{}:
		default:
		}
		return nil
	}, time.Millisecond)
	select {
	case <-wrote:
	case <-time.After(time.Second):
		t.Fatal("heartbeat was not written")
	}
	stop()
	stop()
	stoppedAt := writes.Load()
	time.Sleep(5 * time.Millisecond)
	if got := writes.Load(); got != stoppedAt {
		t.Fatalf("heartbeat continued after stop: before=%d after=%d", stoppedAt, got)
	}
}

func TestSSEHeartbeatExitsWhenConnectionCloses(t *testing.T) {
	attempted := make(chan struct{}, 1)
	var attempts atomic.Int32
	stop := startSSEHeartbeat(func() error {
		attempts.Add(1)
		select {
		case attempted <- struct{}{}:
		default:
		}
		return errors.New("connection closed")
	}, time.Millisecond)
	defer stop()
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("heartbeat write was not attempted")
	}
	time.Sleep(5 * time.Millisecond)
	if got := attempts.Load(); got != 1 {
		t.Fatalf("heartbeat retried after write failure: attempts=%d", got)
	}
}
