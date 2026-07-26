package http

// Internal test package: requestContext and accessLog are unexported, and they
// are what needs pinning down. The handler tests next door use http_test; both
// package forms may live in one directory.

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/example/go-ddd-hex/internal/platform/logging"
)

func TestRequestContextCarriesTheIDIntoTheHandlerAndTheResponse(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestContext)

	var seen string
	r.Get("/x", func(_ http.ResponseWriter, req *http.Request) {
		seen = logging.RequestID(req.Context())
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if seen == "" {
		t.Fatal("the handler saw no request id on its context")
	}
	if got := rec.Header().Get(requestIDHeader); got != seen {
		t.Errorf("response header: got %q, want the same id as the context, %q", got, seen)
	}
}

func TestAnInboundRequestIDIsPreserved(t *testing.T) {
	// Cross-service correlation: an id minted upstream must survive, not be
	// replaced by a fresh one.
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestContext)

	var seen string
	r.Get("/x", func(_ http.ResponseWriter, req *http.Request) {
		seen = logging.RequestID(req.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(requestIDHeader, "from-upstream")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if seen != "from-upstream" {
		t.Errorf("context: got %q, want %q", seen, "from-upstream")
	}
	if got := rec.Header().Get(requestIDHeader); got != "from-upstream" {
		t.Errorf("response header: got %q, want %q", got, "from-upstream")
	}
}

func TestAccessLogRecordsTheRequest(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	r := chi.NewRouter()
	r.Use(accessLog(log))
	r.Post("/api/v1/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil))

	line := decodeLine(t, &buf)
	if got := line["msg"]; got != "request completed" {
		t.Errorf("msg: got %v, want %q", got, "request completed")
	}
	if got := line["method"]; got != http.MethodPost {
		t.Errorf("method: got %v, want %q", got, http.MethodPost)
	}
	if got := line["path"]; got != "/api/v1/orders" {
		t.Errorf("path: got %v, want %q", got, "/api/v1/orders")
	}
	if got := line["status"]; got != float64(http.StatusCreated) {
		t.Errorf("status: got %v, want %d", got, http.StatusCreated)
	}
	if got := line["bytes"]; got != float64(len(`{"ok":true}`)) {
		t.Errorf("bytes: got %v, want %d", got, len(`{"ok":true}`))
	}
	if _, present := line["duration_ms"]; !present {
		t.Error("duration_ms is missing")
	}
}

func TestARequestAndTheWorkItTriggersShareOneRequestID(t *testing.T) {
	// The end-to-end claim: the access line and everything logged deeper during
	// the same request carry the same id, so a single grep reassembles them.
	var buf bytes.Buffer
	log := logging.NewTo(&buf, "info", "json")

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestContext)
	r.Use(accessLog(log))
	r.Post("/api/v1/orders", func(w http.ResponseWriter, req *http.Request) {
		// Stands in for a use case or an outbound adapter: it only has the
		// context, and knows nothing about correlation.
		log.InfoContext(req.Context(), "domain event", slog.String("event", "order.created"))
		w.WriteHeader(http.StatusCreated)
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil))

	lines := decodeLines(t, &buf)
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2 (the domain event and the access line)", len(lines))
	}

	want := rec.Header().Get(requestIDHeader)
	if want == "" {
		t.Fatal("no request id was echoed in the response")
	}
	for i, line := range lines {
		if got := line["request_id"]; got != want {
			t.Errorf("line %d (%v): request_id got %v, want %q", i, line["msg"], got, want)
		}
	}
}

func TestHealthIsNotAccessLogged(t *testing.T) {
	// A liveness probe polled every ten seconds would bury the log. The route is
	// mounted outside the API group precisely to avoid that.
	var buf bytes.Buffer
	srv := NewServer(":0", ServerDeps{}, logging.NewTo(&buf, "info", "json"))

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if buf.Len() != 0 {
		t.Errorf("/health should not be access logged, got: %s", buf.String())
	}
}

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := decodeLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %s", len(lines), buf.String())
	}
	return lines[0]
}

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			t.Fatalf("log output is not valid JSON (%s): %v", buf.String(), err)
		}
		out = append(out, line)
	}
	return out
}
