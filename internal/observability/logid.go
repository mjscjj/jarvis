// Package observability provides the process-wide request correlation helpers.
//
// HTTP entrypoints use the LogID installed by ByteDance Hertz. Background work
// detaches cancellation while preserving that same value; jobs without an
// upstream request mint a standard ByteDance LogID.
package observability

import (
	"context"
	"strings"

	"code.byted.org/gopkg/ctxvalues"
	"code.byted.org/gopkg/logid"
	hertzconsts "code.byted.org/middleware/hertz/byted/consts"
	"code.byted.org/middleware/hertz/pkg/app"
)

const HeaderLogID = hertzconsts.TT_LOGID_HEADER_KEY

// LogID returns the standard ByteDance LogID carried by ctx.
func LogID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctxvalues.LogID(ctx)
	return strings.TrimSpace(value)
}

// WithLogID attaches an existing LogID to ctx.
func WithLogID(ctx context.Context, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return ctxvalues.SetLogID(ctx, strings.TrimSpace(value))
}

// EnsureLogID preserves an existing LogID or mints one with the ByteDance SDK.
func EnsureLogID(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if LogID(ctx) != "" {
		return ctx
	}
	return ctxvalues.SetLogID(ctx, logid.GenLogID())
}

// Detached preserves correlation values for background work without inheriting
// request cancellation or deadlines.
func Detached(ctx context.Context) context.Context {
	if ctx == nil {
		return EnsureLogID(context.Background())
	}
	return EnsureLogID(context.WithoutCancel(ctx))
}

// FromRequestContext reconstructs the standard context value from Hertz's
// RequestContext. The normal server path already has this value; this also keeps
// direct handler tests and manually composed handlers observable.
func FromRequestContext(ctx context.Context, request *app.RequestContext) context.Context {
	if request == nil {
		return EnsureLogID(ctx)
	}
	value := strings.TrimSpace(request.GetString(hertzconsts.LOGIDKEY))
	if value == "" {
		value = strings.TrimSpace(request.Request.Header.Get(hertzconsts.TT_LOGID_HEADER_KEY))
	}
	if value == "" {
		value = strings.TrimSpace(request.Request.Header.Get(hertzconsts.TT_LOGID_HEADER_FALLBACK_KEY))
	}
	if value == "" {
		value = logid.GenLogID()
	}
	request.Set(hertzconsts.LOGIDKEY, value)
	request.Header(hertzconsts.TT_LOGID_HEADER_KEY, value)
	return WithLogID(ctx, value)
}
