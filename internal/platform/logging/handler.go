package logging

import (
	"context"
	"log/slog"
)

// contextHandler decorates a slog.Handler so that every record picks up the
// correlation carried by its context.
//
// This is why slog's *Context methods exist. The codebase already calls
// InfoContext / ErrorContext / WarnContext and already threads the request
// context down through the application layer into the outbound adapters — but
// slog's built-in handlers ignore that context entirely. Reading it here means
// no call site has to know about correlation at all: they pass the context they
// already have, and the request id appears.
type contextHandler struct{ slog.Handler }

// Handle stamps the record with the request id, when there is one.
func (h contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if id := RequestID(ctx); id != "" {
		rec.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, rec)
}

// WithAttrs and WithGroup are NOT boilerplate. slog.Handler's contract is that
// they return a NEW handler, and the embedded implementation returns the
// undecorated one. Without these overrides the first log.With(...) anywhere in
// the codebase would hand back a bare handler and correlation would vanish for
// that logger — silently, with no error and nothing to notice. There is a test
// for exactly this.
func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

// WithGroup preserves the decoration across grouping, for the same reason.
func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
