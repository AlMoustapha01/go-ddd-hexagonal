package shared

import (
	"context"
	"time"
)

// Keyed is implemented by events (and outbox envelopes) that expose the id of
// the aggregate they concern. The read-model projector uses it to know which
// aggregate to reproject without depending on concrete event types.
type Keyed interface {
	AggregateID() string
}

// OutboxRecord is one persisted event awaiting relay to the bus/broker. It is
// written in the same transaction as the state change (see Atomic) and later
// published by the relay.
type OutboxRecord struct {
	ID          string
	EventName   string
	AggregateID string
	Payload     []byte
	OccurredAt  time.Time
}

// OutboxStore is the OUTBOUND PORT for the transactional outbox. Add must run
// inside the caller's transaction (via the context); Fetch/Mark are used by the
// relay, outside that transaction.
type OutboxStore interface {
	// Add appends records, participating in the ambient transaction.
	Add(ctx context.Context, records ...OutboxRecord) error
	// FetchUnpublished returns up to limit records not yet published, oldest first.
	FetchUnpublished(ctx context.Context, limit int) ([]OutboxRecord, error)
	// MarkPublished marks records as successfully published.
	MarkPublished(ctx context.Context, ids ...string) error
}
