package shared

import "context"

// Atomic is the OUTBOUND PORT for a Unit of Work / transaction boundary. It runs
// a function inside a single transaction: every repository and outbox operation
// performed with the context passed to fn participates in that SAME transaction.
// Either everything commits, or nothing does.
//
// This is what makes the Outbox pattern correct: the aggregate change and the
// outbox insert happen in one transaction, so an event can never be persisted
// without its state change, nor lost after it.
type Atomic interface {
	Within(ctx context.Context, fn func(ctx context.Context) error) error
}
