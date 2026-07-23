package observability

import (
	"context"
	"regexp"
	"testing"

	hertzconsts "code.byted.org/middleware/hertz/byted/consts"
	"code.byted.org/middleware/hertz/pkg/app"
)

var standardLogID = regexp.MustCompile(`^02[0-9a-f]{51}$`)

func TestEnsureLogIDUsesByteDanceGenerator(t *testing.T) {
	ctx := EnsureLogID(context.Background())
	if got := LogID(ctx); !standardLogID.MatchString(got) {
		t.Fatalf("LogID() = %q, want standard 53-character ByteDance LogID", got)
	}
}

func TestDetachedPreservesLogID(t *testing.T) {
	parent, cancel := context.WithCancel(WithLogID(context.Background(), "upstream-log-id"))
	cancel()

	ctx := Detached(parent)
	if got := LogID(ctx); got != "upstream-log-id" {
		t.Fatalf("LogID() = %q, want upstream-log-id", got)
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("detached context is cancelled: %v", err)
	}
}

func TestFromRequestContextReusesInboundHeader(t *testing.T) {
	request := &app.RequestContext{}
	request.Request.Header.Set(hertzconsts.TT_LOGID_HEADER_FALLBACK_KEY, "inbound-log-id")

	ctx := FromRequestContext(context.Background(), request)
	if got := LogID(ctx); got != "inbound-log-id" {
		t.Fatalf("LogID() = %q, want inbound-log-id", got)
	}
	if got := string(request.Response.Header.Peek(hertzconsts.TT_LOGID_HEADER_KEY)); got != "inbound-log-id" {
		t.Fatalf("response header = %q, want inbound-log-id", got)
	}
}
