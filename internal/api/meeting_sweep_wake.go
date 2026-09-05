package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// MeetingSweepWaker pulls the next periodic meeting sweep forward.
type MeetingSweepWaker interface {
	TriggerNow()
}

// WakeMeetingSweep is the authenticated machine boundary for Feishu events that
// carry no chat message — a meeting ending, for example — and therefore reach
// Jarvis only through the transport that owns the Bot event channel. It pulls
// the periodic sweep forward and answers immediately: the sweep stays the only
// collector, so an event-driven run and a scheduled run produce the same
// idempotent clues, and a dropped event costs latency rather than evidence.
// The event body is not inspected here; which events are worth a sweep is the
// relay's configuration, and what to collect stays the sweep agent's judgement.
func WakeMeetingSweep(waker MeetingSweepWaker, secret string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if waker == nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50324, fmt.Errorf("meeting sweep wake is unavailable"))
			return
		}
		gotSecret := strings.TrimSpace(string(c.Request.Header.Peek(jarvisRelaySecretHeader)))
		wantSecret := strings.TrimSpace(secret)
		if wantSecret == "" || subtle.ConstantTimeCompare([]byte(gotSecret), []byte(wantSecret)) != 1 {
			writeAPIError(c, consts.StatusUnauthorized, 40124, fmt.Errorf("meeting sweep wake authentication failed"))
			return
		}
		waker.TriggerNow()
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"woken": true}})
	}
}
