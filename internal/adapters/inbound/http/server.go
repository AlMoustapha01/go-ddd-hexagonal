// Package http is the driving HTTP adapter (chi). It wires routes to
// application use cases and exposes an *http.Server.
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	appaccount "github.com/example/go-ddd-hex/internal/application/account"
	apporder "github.com/example/go-ddd-hex/internal/application/order"
	app "github.com/example/go-ddd-hex/internal/application/user"
)

// ServerDeps groups the application ports the HTTP server exposes. Grouping
// them keeps NewServer's signature stable as contexts are added.
type ServerDeps struct {
	UserUC         app.UseCases
	OrderUC        apporder.UseCases
	OrderQueries   apporder.Queries
	AccountUC      appaccount.UseCases
	AccountQueries appaccount.Queries
}

// NewServer builds the fully-routed *http.Server.
func NewServer(addr string, deps ServerDeps, log *slog.Logger) *http.Server {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	r.Get("/health", healthCheck)

	userHandler := NewUserHandler(deps.UserUC, log)
	orderHandler := NewOrderHandler(deps.OrderUC, deps.OrderQueries, log)
	accountHandler := NewAccountHandler(deps.AccountUC, deps.AccountQueries, log)
	r.Route("/api/v1", func(api chi.Router) {
		userHandler.Routes(api)
		orderHandler.Routes(api)
		accountHandler.Routes(api)
	})

	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func healthCheck(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
