// Package observability provides the process-wide request correlation helpers.
//
// HTTP entrypoints get a LogID from Middleware. Background work detaches
// cancellation while preserving that same value; jobs without an upstream
// request mint a new one.
package observability

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

const (
	// HeaderLogID is the response header carrying the LogID, and the request
	// header callers use to propagate their own.
	HeaderLogID = "X-TT-LOGID"
	// headerLogIDFallback is the differently-cased spelling some clients send.
	headerLogIDFallback = "X-Tt-Logid"
	// requestKeyLogID stores the LogID on Hertz's per-request key/value store so
	// handlers reached without Middleware can still find it.
	requestKeyLogID = "K_LOGID"
)

// logIDContextKey keeps the LogID out of reach of unrelated packages storing
// string keys on the same context.
type logIDContextKey struct{}

// LogID returns the LogID carried by ctx.
func LogID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(logIDContextKey{}).(string)
	return strings.TrimSpace(value)
}

// WithLogID attaches an existing LogID to ctx.
func WithLogID(ctx context.Context, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, logIDContextKey{}, strings.TrimSpace(value))
}

// EnsureLogID preserves an existing LogID or mints one.
func EnsureLogID(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if LogID(ctx) != "" {
		return ctx
	}
	return WithLogID(ctx, GenerateLogID())
}

// GenerateLogID mints a value that sorts by time and stays unique within a
// process, so grepping logs by prefix narrows down to a moment.
func GenerateLogID() string {
	return fmt.Sprintf("%013d%016x", time.Now().UTC().UnixMilli(), rand.Uint64())
}

// Detached preserves correlation values for background work without inheriting
// request cancellation or deadlines.
func Detached(ctx context.Context) context.Context {
	if ctx == nil {
		return EnsureLogID(context.Background())
	}
	return EnsureLogID(context.WithoutCancel(ctx))
}

// Middleware installs a LogID on every request and echoes it back, so a caller
// can correlate its own request with what the logs recorded.
func Middleware() app.HandlerFunc {
	return func(ctx context.Context, request *app.RequestContext) {
		request.Next(FromRequestContext(ctx, request))
	}
}

// FromRequestContext reconstructs the standard context value from Hertz's
// RequestContext. The normal server path already has this value; this also keeps
// direct handler tests and manually composed handlers observable.
func FromRequestContext(ctx context.Context, request *app.RequestContext) context.Context {
	if request == nil {
		return EnsureLogID(ctx)
	}
	value := strings.TrimSpace(request.GetString(requestKeyLogID))
	if value == "" {
		value = strings.TrimSpace(request.Request.Header.Get(HeaderLogID))
	}
	if value == "" {
		value = strings.TrimSpace(request.Request.Header.Get(headerLogIDFallback))
	}
	if value == "" {
		value = GenerateLogID()
	}
	request.Set(requestKeyLogID, value)
	request.Header(HeaderLogID, value)
	return WithLogID(ctx, value)
}
