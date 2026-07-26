// Package outbox implements the transactional Outbox pattern: a Publisher that
// stores events durably (in the same transaction as the state change) and a
// Relay that later forwards them to the real event bus/broker.
//
// Why: publishing an event and committing a database change are two separate
// systems with no shared transaction. If the process dies between them, an
// event is lost (or emitted for a change that rolled back). The outbox removes
// that dual-write hazard by turning "save state + emit event" into a single
// database transaction, then relaying the stored events at-least-once.
package outbox

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Publisher implements shared.EventPublisher by writing events into the outbox
// store instead of dispatching them directly. Because Add runs inside the
// caller's transaction (carried on the context), the events are persisted
// atomically with the aggregate change.
type Publisher struct {
	store shared.OutboxStore
	newID func() string
}

// NewPublisher builds an outbox-backed publisher.
//
// Record ids are UUID version 7 (time-ordered) rather than v4. The outbox is
// the busiest table in the system — one row per domain event — so it is where
// the insert locality of a sequential primary key pays off most: rows are
// appended to the rightmost page of the index instead of dirtying a random one
// each time. The relay also drains records oldest-first, which a time-ordered
// key keeps close together on disk.
func NewPublisher(store shared.OutboxStore) *Publisher {
	return &Publisher{store: store, newID: newV7String}
}

// newV7String mints a time-ordered record id. Must does not add a failure mode:
// uuid.NewString is itself Must(NewRandom()).String().
func newV7String() string { return uuid.Must(uuid.NewV7()).String() }

var _ shared.EventPublisher = (*Publisher)(nil)

// envelopePayload is the JSON stored with each record. It is intentionally
// generic: the projector only needs the aggregate id, and richer consumers can
// extend the payload or add a typed codec.
type envelopePayload struct {
	Event       string `json:"event"`
	AggregateID string `json:"aggregate_id"`
	OccurredAt  string `json:"occurred_at"`
}

// Publish serializes each event into an outbox record and stores it.
func (p *Publisher) Publish(ctx context.Context, events ...shared.Event) error {
	if len(events) == 0 {
		return nil
	}
	records := make([]shared.OutboxRecord, 0, len(events))
	for _, e := range events {
		aggID := ""
		if k, ok := e.(shared.Keyed); ok {
			aggID = k.AggregateID()
		}
		payload, err := json.Marshal(envelopePayload{
			Event:       e.EventName(),
			AggregateID: aggID,
			OccurredAt:  e.OccurredAt().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		})
		if err != nil {
			return err
		}
		records = append(records, shared.OutboxRecord{
			ID:          p.newID(),
			EventName:   e.EventName(),
			AggregateID: aggID,
			Payload:     payload,
			OccurredAt:  e.OccurredAt(),
		})
	}
	return p.store.Add(ctx, records...)
}
