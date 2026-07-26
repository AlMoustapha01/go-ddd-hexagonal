package memory

import (
	"context"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Transactor is a no-op shared.Atomic for the in-memory driver: it simply runs
// the function. There is no real transaction, so writes are NOT atomic together
// — acceptable for local dev and tests, but only Postgres provides the true
// all-or-nothing guarantee the Outbox pattern relies on.
type Transactor struct{}

// NewTransactor builds the no-op unit of work.
func NewTransactor() *Transactor { return &Transactor{} }

var _ shared.Atomic = (*Transactor)(nil)

// Within just invokes fn with the same context.
func (Transactor) Within(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
