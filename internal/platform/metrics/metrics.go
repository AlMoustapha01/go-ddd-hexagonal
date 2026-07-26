// Package metrics builds the application's Prometheus collectors and the
// handler that exposes them.
//
// The registry is EXPLICIT rather than prometheus.DefaultRegisterer. A global
// registry is shared process state: registering the same collector twice panics,
// nothing resets between tests, and two instances cannot coexist in one binary.
// Creating the registry in the composition root and injecting it keeps all of
// that under control — the same dependency-injection rule the rest of this
// codebase follows for repositories and buses.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds every collector the application records into.
//
// Note what is NOT here: nothing in the domain or the application layer ever
// sees this type. Instrumentation is applied by decorating ports and by HTTP
// middleware, both of which live in adapters, so business code stays unaware
// that it is being measured.
type Metrics struct {
	registry *prometheus.Registry

	// HTTPRequests and HTTPDuration are the RED signals (rate, errors,
	// duration) of the inbound adapter.
	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec

	// DomainEvents counts business facts, not technical ones: how many orders
	// were placed, how many users registered.
	DomainEvents *prometheus.CounterVec

	// RepoDuration and RepoErrors measure outbound ports, which is where the
	// latency an HTTP histogram cannot attribute usually hides.
	RepoDuration *prometheus.HistogramVec
	RepoErrors   *prometheus.CounterVec
}

// New builds the collectors and registers them on a fresh registry.
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),

		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests served, by method, route pattern and status.",
		}, []string{"method", "route", "status"}),

		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency, by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),

		DomainEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "domain_events_total",
			Help: "Total number of domain events dispatched on the event bus, by event name.",
		}, []string{"event"}),

		RepoDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "repository_operation_duration_seconds",
			Help:    "Outbound port operation latency, by port and operation.",
			Buckets: prometheus.DefBuckets,
		}, []string{"port", "op"}),

		RepoErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "repository_errors_total",
			Help: "Total number of failed outbound port operations, by port and operation.",
		}, []string{"port", "op"}),
	}

	m.registry.MustRegister(
		m.HTTPRequests,
		m.HTTPDuration,
		m.DomainEvents,
		m.RepoDuration,
		m.RepoErrors,
		// Process and Go runtime metrics come free and answer the first
		// questions anyone asks of a service: memory, goroutines, GC, uptime.
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)
	return m
}

// Handler serves the Prometheus exposition format for this registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Registry exposes the underlying registry, so tests can gather and assert on
// what was recorded.
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }
