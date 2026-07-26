// Package integration holds cross-context event handlers. Unlike the individual
// bounded contexts (user, order) — which never import each other — this package
// is explicitly the bridge, so it is allowed to depend on both. Isolating that
// dependency here keeps the reaction logic visible and prevents the contexts
// from leaking into one another.
package integration

import (
	"context"
	"log/slog"

	orderapp "github.com/example/go-ddd-hex/internal/application/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
	user "github.com/example/go-ddd-hex/internal/domain/user"
)

// CancelOrdersOnUserDeactivation reacts to user.Deactivated by cancelling the
// customer's still-open (draft/placed) orders. It reads the open orders from
// the CQRS query side and issues cancel commands to the command side — a clean
// illustration of the two sides cooperating. This is eventual consistency: the
// user change is already committed, and the reaction runs afterwards through
// the event bus, never in the same transaction.
type CancelOrdersOnUserDeactivation struct {
	orders  orderapp.UseCases // command side (to cancel)
	queries orderapp.Queries  // read side (to find open orders)
	log     *slog.Logger
}

// NewCancelOrdersOnUserDeactivation wires the handler with the order command
// and query ports.
func NewCancelOrdersOnUserDeactivation(orders orderapp.UseCases, queries orderapp.Queries, log *slog.Logger) *CancelOrdersOnUserDeactivation {
	return &CancelOrdersOnUserDeactivation{orders: orders, queries: queries, log: log}
}

// Handle matches the eventbus.HandlerFunc signature.
func (h *CancelOrdersOnUserDeactivation) Handle(ctx context.Context, e shared.Event) error {
	ev, ok := e.(user.Deactivated)
	if !ok {
		return nil // not our event; ignore
	}

	summaries, err := h.queries.ListCustomerOrders(ctx, ev.UserID.String())
	if err != nil {
		return err
	}
	for _, s := range summaries {
		if s.Status != "draft" && s.Status != "placed" {
			continue
		}
		if _, err := h.orders.Cancel(ctx, s.ID, "customer deactivated"); err != nil {
			h.log.WarnContext(ctx, "could not cancel order on user deactivation",
				slog.String("order_id", s.ID), slog.Any("error", err))
			continue
		}
		h.log.InfoContext(ctx, "order cancelled due to user deactivation",
			slog.String("order_id", s.ID), slog.String("user_id", ev.UserID.String()))
	}
	return nil
}
