// Package logging builds the application's structured logger (stdlib slog).
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns a configured *slog.Logger writing to stdout. format is "json" or
// "text".
func New(level, format string) *slog.Logger { return NewTo(os.Stdout, level, format) }

// NewTo is New with an explicit destination, so callers that need the output
// elsewhere — a file, a test buffer — get the same logger rather than
// reassembling one and drifting from it.
//
// The handler is wrapped in contextHandler, so any record logged through a
// *Context method carries the request id its context holds. Call sites need to
// do nothing beyond passing the context they already have.
func NewTo(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.ToLower(format) == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(contextHandler{handler})
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
