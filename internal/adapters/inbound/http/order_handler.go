package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	apporder "github.com/example/go-ddd-hex/internal/application/order"
	domainorder "github.com/example/go-ddd-hex/internal/domain/order"
)

// OrderHandler is the inbound HTTP adapter for the order context. It routes
// write requests to the command port (uc) and read requests to the query port
// (queries) — the HTTP layer makes the CQRS split visible at the edge.
type OrderHandler struct {
	uc      apporder.UseCases
	queries apporder.Queries
	log     *slog.Logger
}

// NewOrderHandler wires the handler with both the command and query ports.
func NewOrderHandler(uc apporder.UseCases, queries apporder.Queries, log *slog.Logger) *OrderHandler {
	return &OrderHandler{uc: uc, queries: queries, log: log}
}

// Routes mounts the order endpoints (commands write, queries read).
func (h *OrderHandler) Routes(r chi.Router) {
	// Command side (writes).
	r.Post("/orders", h.create)
	r.Post("/orders/{id}/lines", h.addLine)
	r.Post("/orders/{id}/place", h.place)
	r.Post("/orders/{id}/pay", h.pay)
	r.Post("/orders/{id}/cancel", h.cancel)
	// Query side (reads) — served from the CQRS read model.
	r.Get("/orders", h.list)
	r.Get("/orders/{id}", h.getByID)
	r.Get("/customers/{customerID}/orders", h.listByCustomer)
}

type createOrderRequest struct {
	CustomerID string `json:"customer_id"`
	Currency   string `json:"currency"`
}

type addLineRequest struct {
	ProductID      string `json:"product_id"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
	Currency       string `json:"currency"`
	Quantity       int    `json:"quantity"`
}

type cancelRequest struct {
	Reason string `json:"reason"`
}

func (h *OrderHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if !decode(w, r, &req) {
		return
	}
	view, err := h.uc.Create(r.Context(), apporder.CreateCommand{CustomerID: req.CustomerID, Currency: req.Currency})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *OrderHandler) addLine(w http.ResponseWriter, r *http.Request) {
	var req addLineRequest
	if !decode(w, r, &req) {
		return
	}
	view, err := h.uc.AddLine(r.Context(), apporder.AddLineCommand{
		OrderID:        chi.URLParam(r, "id"),
		ProductID:      req.ProductID,
		UnitPriceMinor: req.UnitPriceMinor,
		Currency:       req.Currency,
		Quantity:       req.Quantity,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *OrderHandler) place(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, h.uc.Place)
}

func (h *OrderHandler) pay(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, h.uc.Pay)
}

func (h *OrderHandler) cancel(w http.ResponseWriter, r *http.Request) {
	var req cancelRequest
	// Body is optional for cancel.
	_ = decodeOptional(r, &req)
	view, err := h.uc.Cancel(r.Context(), chi.URLParam(r, "id"), req.Reason)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *OrderHandler) getByID(w http.ResponseWriter, r *http.Request) {
	view, err := h.queries.GetOrder(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *OrderHandler) list(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	views, err := h.queries.ListOrders(r.Context(), limit, offset)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

func (h *OrderHandler) listByCustomer(w http.ResponseWriter, r *http.Request) {
	views, err := h.queries.ListCustomerOrders(r.Context(), chi.URLParam(r, "customerID"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

// transition factors out the id-only state-change endpoints (place/pay).
func (h *OrderHandler) transition(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id string) (apporder.OrderView, error)) {
	view, err := fn(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *OrderHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domainorder.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: err.Error()})
	case apporder.IsValidationError(err):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
	default:
		h.log.ErrorContext(r.Context(), "unhandled order error", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}
