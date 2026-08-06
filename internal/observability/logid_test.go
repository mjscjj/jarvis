package observability

import (
	"context"
	"regexp"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

var generatedLogID = regexp.MustCompile(`^\d{13}[0-9a-f]{16}$`)

func TestEnsureLogIDMintsSortableValue(t *testing.T) {
	ctx := EnsureLogID(context.Background())
	if got := LogID(ctx); !generatedLogID.MatchString(got) {
		t.Fatalf("LogID() = %q, want millisecond timestamp followed by random hex", got)
	}
}

func TestGenerateLogIDDoesNotRepeat(t *testing.T) {
	seen := make(map[string]struct{}, 128)
	for range 128 {
		value := GenerateLogID()
		if _, duplicate := seen[value]; duplicate {
			t.Fatalf("GenerateLogID() returned %q twice", value)
		}
		seen[value] = struct{}{}
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
	request.Request.Header.Set(headerLogIDFallback, "inbound-log-id")

	ctx := FromRequestContext(context.Background(), request)
	if got := LogID(ctx); got != "inbound-log-id" {
		t.Fatalf("LogID() = %q, want inbound-log-id", got)
	}
	if got := string(request.Response.Header.Peek(HeaderLogID)); got != "inbound-log-id" {
		t.Fatalf("response header = %q, want inbound-log-id", got)
	}
}
