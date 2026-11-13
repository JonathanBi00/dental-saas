// Package reqctx carries per-request metadata (request ID, client IP, user agent) through
// context.Context so lower layers such as the audit log can record it.
package reqctx

import "context"

type Meta struct {
	RequestID string
	IP        string
	UserAgent string
}

type key struct{}

func With(ctx context.Context, m Meta) context.Context { return context.WithValue(ctx, key{}, m) }

func From(ctx context.Context) Meta {
	m, _ := ctx.Value(key{}).(Meta)
	return m
}
