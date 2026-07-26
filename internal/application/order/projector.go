package order

import (
	"context"
	"errors"
	"log/slog"

	domain "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Projector is the bridge between the write and read sides. It subscribes to
// order domain events on the bus and, for each one, rebuilds that order's read
// projection from the write model and upserts it into the ReadStore.
//
// This keeps the read model eventually consistent with the write model. With
// the synchronous in-process bus used here, the projection is updated within
// the same request (read-your-writes); swap in an async bus and it becomes
// eventually consistent — the query code does not change either way.
//
// Loading from the write Repository on each event (rather than reconstructing
// state purely from event payloads) keeps events lean and the projector simple;
// the trade-off and alternatives (fat events, event sourcing, an Outbox) are
// discussed in the README.
type Projector struct {
	writeRepo domain.Repository
	read      ReadStore
	log       *slog.Logger
}

// NewProjector wires the projector with the write repository (source of truth)
// and the read store (destination).
func NewProjector(writeRepo domain.Repository, read ReadStore, log *slog.Logger) *Projector {
	return &Projector{writeRepo: writeRepo, read: read, log: log}
}

// EventNames lists the events the projector must be subscribed to.
func (p *Projector) EventNames() []string {
	return []string{"order.created", "order.line_added", "order.placed", "order.paid", "order.cancelled"}
}

// Handle matches eventbus.HandlerFunc. It reprojects the affected order. The
// event may be a concrete order event or an outbox envelope relayed from the
// store — both expose the aggregate id via shared.Keyed, so the projector does
// not depend on concrete event types.
func (p *Projector) Handle(ctx context.Context, e shared.Event) error {
	keyed, ok := e.(shared.Keyed)
	if !ok {
		return nil // not a keyed (order) event
	}
	id, err := domain.IDFromString(keyed.AggregateID())
	if err != nil {
		return nil // not an order aggregate id
	}

	o, err := p.writeRepo.FindByID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		// The order was deleted; drop its projection.
		return p.read.Delete(ctx, id.String())
	}
	if err != nil {
		return err
	}

	if err := p.read.Upsert(ctx, projectDetail(o)); err != nil {
		p.log.ErrorContext(ctx, "projection upsert failed",
			slog.String("order_id", id.String()), slog.Any("error", err))
		return err
	}
	return nil
}
