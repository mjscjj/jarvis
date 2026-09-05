package api

import (
	"bytes"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type meetingSweepWakerStub struct {
	woken atomic.Int64
}

func (s *meetingSweepWakerStub) TriggerNow() { s.woken.Add(1) }

func TestWakeMeetingSweepAcceptsRelayedEvent(t *testing.T) {
	waker := &meetingSweepWakerStub{}
	h := server.New()
	h.POST("/internal/meeting-sweep/wake", WakeMeetingSweep(waker, "relay-secret"))
	body := []byte(`{"schema":"2.0","header":{"event_type":"vc.meeting.participant_meeting_ended_v1"}}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/meeting-sweep/wake", &ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: jarvisRelaySecretHeader, Value: "relay-secret"},
	).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if got := waker.woken.Load(); got != 1 {
		t.Fatalf("woken = %d, want 1", got)
	}
}

func TestWakeMeetingSweepRejectsWrongSecret(t *testing.T) {
	waker := &meetingSweepWakerStub{}
	h := server.New()
	h.POST("/internal/meeting-sweep/wake", WakeMeetingSweep(waker, "relay-secret"))
	body := []byte(`{}`)
	response := ut.PerformRequest(
		h.Engine, "POST", "/internal/meeting-sweep/wake", &ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: jarvisRelaySecretHeader, Value: "wrong"},
	).Result()
	if response.StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	if got := waker.woken.Load(); got != 0 {
		t.Fatalf("woken = %d, want 0", got)
	}
}
