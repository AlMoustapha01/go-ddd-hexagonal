package order

import (
	"context"
	"time"

	domain "github.com/example/go-ddd-hex/internal/domain/order"
)

// ===========================================================================
// READ SIDE (CQRS query models).
//
// CQRS = Command Query Responsibility Segregation. The write side (commands +
// aggregate + Repository) and the read side (queries + these projections +
// ReadStore) are deliberately SEPARATE: different models, a different store,
// and a different code path. The read models below are denormalized and shaped
// for querying — no behavior, no invariants, just data optimized for display.
// They are kept up to date by the Projector, which listens to domain events.
// ===========================================================================

// SummaryView is the lightweight list projection of an order.
type SummaryView struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customer_id"`
	Status     string    `json:"status"`
	Currency   string    `json:"currency"`
	TotalMinor int64     `json:"total_minor"`
	ItemCount  int       `json:"item_count"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DetailView is the full single-order projection (summary + lines).
type DetailView struct {
	SummaryView
	Lines     []LineView `json:"lines"`
	CreatedAt time.Time  `json:"created_at"`
}

// ReadStore is the OUTBOUND PORT for the read side. It is a store SEPARATE from
// the write Repository — that separation is the essence of CQRS. In this
// boilerplate the in-memory and Postgres adapters back it; in a larger system
// it could be Elasticsearch, a materialized view, or a replica tuned for reads.
type ReadStore interface {
	// Upsert inserts or replaces a projected order (called by the projector).
	Upsert(ctx context.Context, d DetailView) error
	// Delete removes a projection.
	Delete(ctx context.Context, id string) error
	// Get returns a full detail projection, or domain order.ErrNotFound.
	Get(ctx context.Context, id string) (DetailView, error)
	// List returns summaries, newest first, with pagination.
	List(ctx context.Context, limit, offset int) ([]SummaryView, error)
	// ListByCustomer returns a customer's order summaries, newest first.
	ListByCustomer(ctx context.Context, customerID string) ([]SummaryView, error)
}

// projectDetail maps a write-side aggregate to a read-side detail projection.
// It lives on the read side because it defines the shape of the read model.
func projectDetail(o *domain.Order) DetailView {
	lines := make([]LineView, 0, len(o.Lines()))
	for _, l := range o.Lines() {
		lines = append(lines, LineView{
			ProductID: l.ProductID(),
			UnitPrice: l.UnitPrice().Amount(),
			Currency:  l.UnitPrice().Currency(),
			Quantity:  l.Quantity(),
			Subtotal:  l.Subtotal().Amount(),
		})
	}
	total, _ := o.Total()
	return DetailView{
		SummaryView: SummaryView{
			ID:         o.ID().String(),
			CustomerID: o.CustomerID().String(),
			Status:     string(o.Status()),
			Currency:   o.Currency(),
			TotalMinor: total.Amount(),
			ItemCount:  len(o.Lines()),
			UpdatedAt:  o.UpdatedAt(),
		},
		Lines:     lines,
		CreatedAt: o.CreatedAt(),
	}
}

// Summary derives the list projection from a detail projection.
func (d DetailView) Summary() SummaryView { return d.SummaryView }
