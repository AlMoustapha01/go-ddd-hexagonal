package account

import (
	"context"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// EventStore is the OUTBOUND PORT for event-sourced persistence. Unlike a
// classic Repository (which saves current state), it appends immutable events
// and loads the full stream.
type EventStore interface {
	// Load returns the ordered event stream for an aggregate, or ErrNotFound if
	// the stream does not exist.
	Load(ctx context.Context, id ID) ([]shared.Event, error)

	// Append atomically appends events to the stream ONLY IF the current stored
	// version equals expectedVersion (optimistic concurrency). Otherwise it
	// returns ErrConcurrencyConflict, so a caller can reload and retry.
	Append(ctx context.Context, id ID, expectedVersion int, events []shared.Event) error
}
