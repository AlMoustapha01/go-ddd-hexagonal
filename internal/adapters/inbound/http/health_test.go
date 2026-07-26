package http

// Internal test package: the probes and the timeout constants are unexported.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/go-ddd-hex/internal/platform/logging"
)

// probe drives one request against a server built with the given readiness
// check, and returns the recorder plus whatever was logged.
func probe(t *testing.T, path string, ready ReadinessCheck) (*httptest.ResponseRecorder, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	srv := NewServer(":0", ServerDeps{Ready: ready}, logging.NewTo(&buf, "info", "json"))

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec, &buf
}

func TestLivenessIgnoresDependencies(t *testing.T) {
	// The distinction that matters: a database outage must not make the process
	// look dead, or the orchestrator kills a pod that would have recovered on
	// its own once the database came back.
	down := func(context.Context) error { return errors.New("database is unreachable") }

	rec, _ := probe(t, "/health", down)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d — liveness must not depend on the database", rec.Code, http.StatusOK)
	}
}

func TestReadinessWithoutACheckIsAlwaysReady(t *testing.T) {
	// The in-memory driver has nothing to probe.
	rec, _ := probe(t, "/ready", nil)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadinessReportsAHealthyDependency(t *testing.T) {
	up := func(context.Context) error { return nil }

	rec, _ := probe(t, "/ready", up)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadinessReports503WhenADependencyIsDown(t *testing.T) {
	// 503 and not 500: it tells the orchestrator to stop routing here while
	// leaving the process alive.
	down := func(context.Context) error { return errors.New("database is unreachable") }

	rec, logs := probe(t, "/ready", down)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !bytes.Contains(logs.Bytes(), []byte("readiness check failed")) {
		t.Errorf("the failure should be logged, got: %s", logs.String())
	}
}

func TestReadinessCarriesADeadline(t *testing.T) {
	// A wedged database must produce a prompt 503, not a probe that hangs until
	// the orchestrator's own timeout.
	var hadDeadline bool
	check := func(ctx context.Context) error {
		_, hadDeadline = ctx.Deadline()
		return nil
	}

	if _, _ = probe(t, "/ready", check); !hadDeadline {
		t.Error("the readiness check ran without a deadline")
	}
}

func TestProbesAreNotAccessLogged(t *testing.T) {
	// Polled every ten seconds, they would bury the log.
	for _, path := range []string{"/health", "/ready"} {
		rec, logs := probe(t, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status got %d, want %d", path, rec.Code, http.StatusOK)
		}
		if logs.Len() != 0 {
			t.Errorf("%s should not be access logged, got: %s", path, logs.String())
		}
	}
}

func TestWriteTimeoutExceedsHandlerTimeout(t *testing.T) {
	// chi's Timeout middleware writes its 504 from a defer, after the handler
	// returns. If the connection's write deadline expired at that same moment
	// the client would get a dropped connection instead of the 504. Adjusting
	// either constant without preserving this ordering breaks that silently.
	if writeTimeout <= handlerTimeout {
		t.Fatalf("writeTimeout (%s) must exceed handlerTimeout (%s)", writeTimeout, handlerTimeout)
	}
}
