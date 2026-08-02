package capture

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartMessageEventConsumerWaitsForReadyAndStopsByEOF(t *testing.T) {
	script := writeEventConsumerFixture(t, `#!/bin/sh
printf '%s\n' '[event] connecting' >&2
printf '%s\n' '[event] ready event_key=im.message.receive_v1' >&2
printf '%s\n' '{"type":"im.message.receive_v1","message_id":"om_fixture"}'
cat >/dev/null
`)
	appender := &recordingMessageEventAppender{received: make(chan struct{})}
	consumer, err := StartMessageEventConsumer(
		context.Background(), appender,
		MessageEventConsumerOptions{Bin: script, Profile: "cli_fixture", ReadyTimeout: 2 * time.Second},
		log.New(io.Discard, "", 0),
	)
	if err != nil {
		t.Fatalf("StartMessageEventConsumer() error = %v", err)
	}
	select {
	case <-appender.received:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not deliver stdout event")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := consumer.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := consumer.Err(); err != nil {
		t.Fatalf("consumer.Err() = %v", err)
	}
	if got := string(appender.Raw()); !strings.Contains(got, `"message_id":"om_fixture"`) {
		t.Fatalf("appender raw = %q", got)
	}
}

func TestStartMessageEventConsumerSurfacesPreReadyFailure(t *testing.T) {
	script := writeEventConsumerFixture(t, `#!/bin/sh
printf '%s\n' '{"ok":false,"error":{"type":"validation","subtype":"failed_precondition"}}' >&2
exit 2
`)
	_, err := StartMessageEventConsumer(
		context.Background(), &recordingMessageEventAppender{},
		MessageEventConsumerOptions{Bin: script, Profile: "cli_fixture", ReadyTimeout: 2 * time.Second},
		log.New(io.Discard, "", 0),
	)
	if err == nil || !strings.Contains(err.Error(), "failed_precondition") {
		t.Fatalf("StartMessageEventConsumer() error = %v, want structured stderr", err)
	}
}

func writeEventConsumerFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-lark-cli")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write fake lark-cli: %v", err)
	}
	return path
}

type recordingMessageEventAppender struct {
	mu       sync.Mutex
	raw      json.RawMessage
	received chan struct{}
	once     sync.Once
}

func (a *recordingMessageEventAppender) AppendMessageEvent(_ context.Context, raw json.RawMessage) (*MessageEventResult, error) {
	a.mu.Lock()
	a.raw = append(a.raw[:0], raw...)
	a.mu.Unlock()
	if a.received != nil {
		a.once.Do(func() { close(a.received) })
	}
	return &MessageEventResult{MessageID: "om_fixture", ChatID: "oc_fixture", Inserted: true, Related: true}, nil
}

func (a *recordingMessageEventAppender) Raw() json.RawMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append(json.RawMessage(nil), a.raw...)
}
