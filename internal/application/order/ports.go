package order

import "context"

// UseCases is the INBOUND PORT for the COMMAND side of the order context. It
// only exposes state-changing operations; reads live on the Queries port
// (query_service.go). That command/query split is the C-Q of CQRS.
type UseCases interface {
	Create(ctx context.Context, cmd CreateCommand) (OrderView, error)
	AddLine(ctx context.Context, cmd AddLineCommand) (OrderView, error)
	Place(ctx context.Context, id string) (OrderView, error)
	Pay(ctx context.Context, id string) (OrderView, error)
	Cancel(ctx context.Context, id, reason string) (OrderView, error)
}

// Dispatcher is an OUTBOUND PORT: a hint to flush the outbox right after a
// command commits, for low-latency delivery. The relay implements it. It is
// only a hint — the relay's background poller is the durability backstop, so a
// missed or failed dispatch here is retried later, never lost.
type Dispatcher interface {
	Dispatch(ctx context.Context) error
}

// CustomerChecker is an OUTBOUND PORT the order use cases use to enforce a
// cross-aggregate rule ("the customer must exist and be active") WITHOUT
// depending on the user package's internals. The bootstrap layer provides an
// implementation backed by the user repository. This keeps the two bounded
// contexts decoupled: order talks to a narrow interface, not to user's domain.
type CustomerChecker interface {
	IsActiveCustomer(ctx context.Context, customerID string) (bool, error)
}
