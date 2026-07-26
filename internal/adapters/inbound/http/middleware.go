package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/example/go-ddd-hex/internal/platform/logging"
)

// requestIDHeader is both read (an upstream service may already have minted an
// id) and written (so a client can quote it in a bug report). chi's RequestID
// middleware honours the inbound header on its own; echoing it back is ours.
const requestIDHeader = "X-Request-Id"

// requestContext bridges chi's request id into the platform's context, where the
// logger looks for it.
//
// The translation lives here rather than in platform/logging on purpose: chi is
// a transport detail, and the platform packages must not depend on the web
// framework. This middleware is the small anti-corruption step between the two,
// and the only place in the codebase that knows both keys.
//
// It must be mounted AFTER middleware.RequestID, which is what puts the id on
// the context in the first place.
func requestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := middleware.GetReqID(r.Context())
		if id != "" {
			w.Header().Set(requestIDHeader, id)
		}
		next.ServeHTTP(w, r.WithContext(logging.WithRequestID(r.Context(), id)))
	})
}

// accessLog emits one line per request: the anchor that everything else logged
// during that request correlates to.
//
// Without it, a successful request produces no log at all and there is nothing
// for the request id to tie together.
func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WrapResponseWriter records the status and byte count, which the
			// bare http.ResponseWriter does not expose after the fact.
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			// Deferred so the line is still written when a handler panics:
			// Recoverer sits upstream and turns the panic into a 500, which
			// this observes on the way out.
			defer func() {
				log.InfoContext(r.Context(), "request completed",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", ww.Status()),
					slog.Int("bytes", ww.BytesWritten()),
					slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				)
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
