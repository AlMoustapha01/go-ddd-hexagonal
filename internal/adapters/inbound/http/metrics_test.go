package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/example/go-ddd-hex/internal/platform/logging"
	appmetrics "github.com/example/go-ddd-hex/internal/platform/metrics"
)

// labelValues gathers one label across every series of a metric family, which
// is what makes it possible to assert on the label VALUES rather than merely on
// a count.
func labelValues(t *testing.T, m *appmetrics.Metrics, family, label string) []string {
	t.Helper()
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	var out []string
	for _, f := range families {
		if f.GetName() != family {
			continue
		}
		for _, series := range f.GetMetric() {
			for _, pair := range series.GetLabel() {
				if pair.GetName() == label {
					out = append(out, pair.GetValue())
				}
			}
		}
	}
	return out
}

// routerWithMetrics mounts a parameterised route behind the recording
// middleware.
func routerWithMetrics(m *appmetrics.Metrics) chi.Router {
	r := chi.NewRouter()
	r.Use(recordMetrics(m))
	r.Get("/api/v1/orders/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Post("/api/v1/orders", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	return r
}

func TestTheRouteLabelIsThePatternNotThePath(t *testing.T) {
	// The cardinality guard, and the reason this middleware exists rather than
	// an off-the-shelf one. With r.URL.Path, every order id would mint its own
	// time series; a few thousand orders and Prometheus falls over. Recording
	// the chi pattern keeps the series count bounded by the number of routes.
	m := appmetrics.New()
	r := routerWithMetrics(m)

	// Three DIFFERENT ids down the same route.
	for i := 0; i < 3; i++ {
		id := uuid.Must(uuid.NewV7()).String()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+id, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
		}
	}

	routes := labelValues(t, m, "http_requests_total", "route")
	if len(routes) != 1 {
		t.Fatalf("three requests down one route produced %d time series (%v) — the route label is leaking ids", len(routes), routes)
	}
	if routes[0] != "/api/v1/orders/{id}" {
		t.Errorf("route label: got %q, want %q", routes[0], "/api/v1/orders/{id}")
	}
	for _, r := range routes {
		if strings.Count(r, "-") >= 4 { // a UUID has four hyphens
			t.Errorf("the route label contains what looks like an id: %q", r)
		}
	}
}

func TestUnmatchedRequestsShareOneLabel(t *testing.T) {
	// Same hazard from the other direction: a scanner walking random URLs must
	// not be able to grow the metric without bound.
	m := appmetrics.New()
	r := routerWithMetrics(m)

	for _, path := range []string{"/nope", "/also-nope", "/wp-admin"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	routes := labelValues(t, m, "http_requests_total", "route")
	if len(routes) != 1 {
		t.Fatalf("three unmatched paths produced %d time series (%v), want 1", len(routes), routes)
	}
	if routes[0] != unmatchedRoute {
		t.Errorf("route label: got %q, want %q", routes[0], unmatchedRoute)
	}
}

func TestMethodAndStatusAreRecorded(t *testing.T) {
	m := appmetrics.New()
	r := routerWithMetrics(m)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil))

	if got := labelValues(t, m, "http_requests_total", "method"); len(got) != 1 || got[0] != http.MethodPost {
		t.Errorf("method label: got %v, want [POST]", got)
	}
	if got := labelValues(t, m, "http_requests_total", "status"); len(got) != 1 || got[0] != "201" {
		t.Errorf("status label: got %v, want [201]", got)
	}
}

func TestDurationIsObserved(t *testing.T) {
	m := appmetrics.New()
	r := routerWithMetrics(m)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil))

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != "http_request_duration_seconds" {
			continue
		}
		if n := f.GetMetric()[0].GetHistogram().GetSampleCount(); n != 1 {
			t.Errorf("sample count: got %d, want 1", n)
		}
		return
	}
	t.Fatal("http_request_duration_seconds was never recorded")
}

func TestMetricsEndpointServesAndIsNotSelfCounted(t *testing.T) {
	m := appmetrics.New()
	var logs strings.Builder
	srv := NewServer(":0", ServerDeps{Metrics: m}, logging.NewTo(&logs, "info", "json"))

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	// A CounterVec with no observed label combination exports nothing, so the
	// runtime collectors are what proves the registry is wired and served.
	if !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Errorf("the exposition should carry the Go runtime collectors, got: %s", rec.Body.String())
	}
	// Scraped every fifteen seconds, it must neither count itself nor skew the
	// latency histogram, and it must not flood the access log.
	if routes := labelValues(t, m, "http_requests_total", "route"); len(routes) != 0 {
		t.Errorf("the scrape counted itself: %v", routes)
	}
	if logs.Len() != 0 {
		t.Errorf("/metrics should not be access logged, got: %s", logs.String())
	}
}

func TestAServerWithoutMetricsStillWorks(t *testing.T) {
	// ServerDeps.Metrics is optional, which is what lets the probe tests build a
	// server without wiring observability.
	srv := NewServer(":0", ServerDeps{}, logging.NewTo(&strings.Builder{}, "info", "json"))

	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/health: got %d, want %d", rec.Code, http.StatusOK)
	}

	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/metrics without collectors: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}
