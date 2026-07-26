// Package http is the driving HTTP adapter (chi). It wires routes to
// application use cases and exposes an *http.Server.
package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	appaccount "github.com/example/go-ddd-hex/internal/application/account"
	apporder "github.com/example/go-ddd-hex/internal/application/order"
	app "github.com/example/go-ddd-hex/internal/application/user"
	appmetrics "github.com/example/go-ddd-hex/internal/platform/metrics"
)

const (
	// handlerTimeout bounds how long a handler may run before chi's Timeout
	// middleware gives up on it.
	handlerTimeout = 15 * time.Second

	// writeTimeout MUST EXCEED handlerTimeout. chi's Timeout middleware writes
	// its 504 from a defer, i.e. after the handler returns; if the connection's
	// write deadline expired at that same instant, the timeout response would
	// never reach the client, who would see a dropped connection instead of an
	// explicit 504. The test asserting this relation is what keeps a future
	// tweak from quietly breaking it.
	writeTimeout = handlerTimeout + 5*time.Second

	// readinessTimeout bounds the dependency probe, so a wedged database
	// produces a prompt 503 rather than a hanging request.
	readinessTimeout = 2 * time.Second
)

// ReadinessCheck reports whether the dependencies needed to serve traffic are
// reachable. A nil check means there is nothing to check — the in-memory driver
// has no external dependency.
//
// It is a narrow port on purpose: the HTTP adapter must not import the
// persistence driver to find out whether the database answers.
type ReadinessCheck func(ctx context.Context) error

// ServerDeps groups the application ports the HTTP server exposes. Grouping
// them keeps NewServer's signature stable as contexts are added.
type ServerDeps struct {
	UserUC         app.UseCases
	OrderUC        apporder.UseCases
	OrderQueries   apporder.Queries
	AccountUC      appaccount.UseCases
	AccountQueries appaccount.Queries

	// Ready probes the outbound dependencies. Optional; nil means always ready.
	Ready ReadinessCheck

	// Metrics carries the collectors. Optional: nil leaves /metrics unmounted
	// and the recording middleware off, which is what lets a test build a
	// server without wiring observability.
	Metrics *appmetrics.Metrics
}

// NewServer builds the fully-routed *http.Server.
func NewServer(addr string, deps ServerDeps, log *slog.Logger) *http.Server {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(requestContext) // must follow RequestID: it reads what chi just set
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(handlerTimeout))

	// Both probes sit deliberately outside the access log: polled every ten
	// seconds they would add roughly 8,600 lines of noise a day each. Mounting
	// the access log on the API group below is enough — no exclusion list to
	// maintain.
	//
	// Two probes, not one, following the Kubernetes convention:
	//   /health — liveness:  the process answers, so do not restart it.
	//   /ready  — readiness: its dependencies answer, so send it traffic.
	// Conflating them means a pod whose database is unreachable keeps taking
	// requests it cannot serve, and a transient database blip gets the pod
	// killed instead of merely drained.
	r.Get("/health", healthCheck)
	r.Get("/ready", readinessHandler(deps.Ready, log))

	// /metrics joins the probes outside both the access log and the recording
	// middleware: a scrape every fifteen seconds has no business counting
	// itself, nor skewing the latency histogram with its own duration.
	if deps.Metrics != nil {
		r.Method(http.MethodGet, "/metrics", deps.Metrics.Handler())
	}

	userHandler := NewUserHandler(deps.UserUC, log)
	orderHandler := NewOrderHandler(deps.OrderUC, deps.OrderQueries, log)
	accountHandler := NewAccountHandler(deps.AccountUC, deps.AccountQueries, log)
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(accessLog(log))
		if deps.Metrics != nil {
			api.Use(recordMetrics(deps.Metrics))
		}
		userHandler.Routes(api)
		orderHandler.Routes(api)
		accountHandler.Routes(api)
	})

	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       60 * time.Second,
	}
}

// readinessHandler answers whether this instance can serve traffic.
//
// It returns 503 rather than 500 when a dependency is down: 503 tells an
// orchestrator to stop routing requests here while leaving the process alive,
// which is the whole point of separating readiness from liveness.
func readinessHandler(check ReadinessCheck, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if check == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		if err := check(ctx); err != nil {
			log.WarnContext(r.Context(), "readiness check failed", slog.Any("error", err))
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func healthCheck(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
