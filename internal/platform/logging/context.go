package logging

import "context"

// requestIDKey is unexported and of a dedicated type, so no other package can
// collide with it or overwrite the value by accident.
type requestIDKey struct{}

// WithRequestID returns a context carrying the id of the request being served.
//
// It is set once, at the edge, by the inbound HTTP adapter. Everything below —
// use cases, aggregates, outbound adapters — receives it for free by passing the
// context along, which the codebase already does.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request id carried by ctx, or "" outside a request —
// during start-up, or in a background worker such as the outbox relay.
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
