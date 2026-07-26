package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	appaccount "github.com/example/go-ddd-hex/internal/application/account"
	domainaccount "github.com/example/go-ddd-hex/internal/domain/account"
)

// AccountHandler is the inbound HTTP adapter for the event-sourced account
// context. Writes go through the command port; reads through the query port
// (which folds the event stream).
type AccountHandler struct {
	uc      appaccount.UseCases
	queries appaccount.Queries
	log     *slog.Logger
}

// NewAccountHandler wires the handler with the command and query ports.
func NewAccountHandler(uc appaccount.UseCases, queries appaccount.Queries, log *slog.Logger) *AccountHandler {
	return &AccountHandler{uc: uc, queries: queries, log: log}
}

// Routes mounts the account endpoints.
func (h *AccountHandler) Routes(r chi.Router) {
	r.Post("/accounts", h.open)
	r.Post("/accounts/{id}/deposit", h.deposit)
	r.Post("/accounts/{id}/withdraw", h.withdraw)
	r.Get("/accounts/{id}", h.get)
	r.Get("/accounts/{id}/history", h.history)
}

type openRequest struct {
	Currency string `json:"currency"`
}

type amountRequest struct {
	AmountMinor int64 `json:"amount_minor"`
}

func (h *AccountHandler) open(w http.ResponseWriter, r *http.Request) {
	var req openRequest
	if !decode(w, r, &req) {
		return
	}
	view, err := h.uc.Open(r.Context(), appaccount.OpenCommand{Currency: req.Currency})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *AccountHandler) deposit(w http.ResponseWriter, r *http.Request) {
	h.amountOp(w, r, h.uc.Deposit)
}

func (h *AccountHandler) withdraw(w http.ResponseWriter, r *http.Request) {
	h.amountOp(w, r, h.uc.Withdraw)
}

func (h *AccountHandler) amountOp(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, cmd appaccount.AmountCommand) (appaccount.AccountView, error)) {
	var req amountRequest
	if !decode(w, r, &req) {
		return
	}
	view, err := fn(r.Context(), appaccount.AmountCommand{AccountID: chi.URLParam(r, "id"), Amount: req.AmountMinor})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *AccountHandler) get(w http.ResponseWriter, r *http.Request) {
	view, err := h.queries.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *AccountHandler) history(w http.ResponseWriter, r *http.Request) {
	entries, err := h.queries.History(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (h *AccountHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domainaccount.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: err.Error()})
	case errors.Is(err, domainaccount.ErrConcurrencyConflict):
		writeJSON(w, http.StatusConflict, errorResponse{Error: err.Error()})
	case appaccount.IsValidationError(err):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
	default:
		h.log.ErrorContext(r.Context(), "unhandled account error", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}
