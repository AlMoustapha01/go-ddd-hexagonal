package logging

// Internal test package: the decorator and its context key are unexported, and
// they are precisely what needs pinning down.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// newTestLogger builds the same logger New builds, writing to buf instead of
// stdout.
func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(contextHandler{slog.NewJSONHandler(buf, nil)})
}

// decode reads back the single record written to buf.
func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log line is not valid JSON (%s): %v", buf.String(), err)
	}
	return rec
}

func TestRequestIDIsStampedOnRecordsLoggedWithItsContext(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)

	ctx := WithRequestID(context.Background(), "req-42")
	log.InfoContext(ctx, "domain event")

	rec := decode(t, &buf)
	if got := rec["request_id"]; got != "req-42" {
		t.Errorf("request_id: got %v, want %q", got, "req-42")
	}
	if got := rec["msg"]; got != "domain event" {
		t.Errorf("msg: got %v, want %q", got, "domain event")
	}
}

func TestNoRequestIDFieldOutsideARequest(t *testing.T) {
	// Start-up logs and background workers (the outbox relay) have no request
	// id. They must not carry an empty field polluting every line.
	var buf bytes.Buffer
	log := newTestLogger(&buf)

	log.InfoContext(context.Background(), "using postgres repositories")

	rec := decode(t, &buf)
	if _, present := rec["request_id"]; present {
		t.Errorf("request_id should be absent, got %v", rec["request_id"])
	}
}

func TestCorrelationSurvivesWith(t *testing.T) {
	// The trap this whole design turns on. slog.Handler.WithAttrs returns a NEW
	// handler; if contextHandler did not override it, the embedded handler would
	// come back undecorated and every record logged through the derived logger
	// would silently lose its request id.
	var buf bytes.Buffer
	log := newTestLogger(&buf).With(slog.String("component", "projector"))

	ctx := WithRequestID(context.Background(), "req-42")
	log.InfoContext(ctx, "projection upsert failed")

	rec := decode(t, &buf)
	if got := rec["request_id"]; got != "req-42" {
		t.Errorf("request_id lost through With: got %v, want %q", got, "req-42")
	}
	if got := rec["component"]; got != "projector" {
		t.Errorf("component: got %v, want %q", got, "projector")
	}
}

func TestCorrelationSurvivesWithGroup(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf).WithGroup("http")

	ctx := WithRequestID(context.Background(), "req-42")
	log.InfoContext(ctx, "request completed")

	rec := decode(t, &buf)
	// The attribute is added inside the active group, so it nests under it.
	group, ok := rec["http"].(map[string]any)
	if !ok {
		t.Fatalf("expected an \"http\" group, got %v", rec)
	}
	if got := group["request_id"]; got != "req-42" {
		t.Errorf("request_id lost through WithGroup: got %v, want %q", got, "req-42")
	}
}

func TestNewReturnsADecoratedLogger(t *testing.T) {
	// Guards the wiring itself: building the handler and forgetting to wrap it
	// would leave every test above passing while production logs stayed bare.
	if _, ok := New("info", "json").Handler().(contextHandler); !ok {
		t.Fatalf("New must wrap its handler in contextHandler, got %T", New("info", "json").Handler())
	}
}

func TestRequestIDRoundTrip(t *testing.T) {
	t.Run("returns the stored id", func(t *testing.T) {
		ctx := WithRequestID(context.Background(), "req-42")
		if got := RequestID(ctx); got != "req-42" {
			t.Errorf("got %q, want %q", got, "req-42")
		}
	})

	t.Run("returns empty for a bare context", func(t *testing.T) {
		if got := RequestID(context.Background()); got != "" {
			t.Errorf("got %q, want an empty string", got)
		}
	})

	t.Run("storing an empty id is a no-op", func(t *testing.T) {
		ctx := WithRequestID(context.Background(), "")
		if got := RequestID(ctx); got != "" {
			t.Errorf("got %q, want an empty string", got)
		}
	})

	t.Run("tolerates a nil context", func(t *testing.T) {
		//nolint:staticcheck // passing nil is exactly what is under test
		if got := RequestID(nil); got != "" {
			t.Errorf("got %q, want an empty string", got)
		}
	})
}
