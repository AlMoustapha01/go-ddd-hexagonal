package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	appmetrics "github.com/example/go-ddd-hex/internal/platform/metrics"
)

// unmatchedRoute is the single label value used for requests that matched no
// route. Without it, a scanner walking random URLs would mint one time series
// per URL and the metric would grow without bound.
const unmatchedRoute = "unmatched"

// recordMetrics records the RED signals of the inbound adapter: request rate,
// error rate (through the status label) and duration.
//
// The route label is chi's ROUTE PATTERN — "/api/v1/orders/{id}" — never the raw
// path. That distinction is the whole game: with r.URL.Path, every order id
// would create its own time series and a few thousand orders would take
// Prometheus down. It is the classic mistake of HTTP instrumentation.
//
// Worth contrasting with accessLog next door, which deliberately records the RAW
// path. The two want opposite things from the same datum: a log exists to debug
// THIS request, a metric to aggregate ALL of them.
func recordMetrics(m *appmetrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			next.ServeHTTP(ww, r)

			// Read AFTER the handler: chi only fills the route pattern in once
			// it has matched, which happens downstream of this middleware.
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = unmatchedRoute
			}

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK // handler returned without writing
			}

			m.HTTPRequests.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
			m.HTTPDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		})
	}
}
