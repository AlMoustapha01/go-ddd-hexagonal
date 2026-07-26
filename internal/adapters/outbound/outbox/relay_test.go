package outbox_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/outbox"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// stubEvent is a minimal keyed event for testing the outbox path.
type stubEvent struct {
	name string
	agg  string
}

func (e stubEvent) EventName() string     { return e.name }
func (e stubEvent) OccurredAt() time.Time { return time.Unix(0, 0).UTC() }
func (e stubEvent) AggregateID() string   { return e.agg }

// captureBus records everything the relay publishes.
type captureBus struct {
	events   []shared.Event
	failNext bool
}

func (b *captureBus) Publish(_ context.Context, events ...shared.Event) error {
	if b.failNext {
		b.failNext = false
		return context.DeadlineExceeded
	}
	b.events = append(b.events, events...)
	return nil
}

func TestOutboxWriteThenRelay(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError + 1}))

	store := memory.NewOutboxStore()
	pub := outbox.NewPublisher(store)
	bus := &captureBus{}
	relay := outbox.NewRelay(store, bus, log)

	// Publish two events -> they go to the store, not the bus yet.
	if err := pub.Publish(ctx, stubEvent{"order.created", "o-1"}, stubEvent{"order.placed", "o-1"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(bus.events) != 0 {
		t.Fatalf("events must not reach the bus before relay, got %d", len(bus.events))
	}

	// Relay drains the store to the bus.
	if err := relay.Dispatch(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(bus.events) != 2 {
		t.Fatalf("relay should deliver 2 events, got %d", len(bus.events))
	}
	// The delivered events are keyed with the aggregate id (as envelopes).
	if k, ok := bus.events[0].(shared.Keyed); !ok || k.AggregateID() != "o-1" {
		t.Fatalf("delivered event should be keyed with aggregate id o-1")
	}

	// A second drain delivers nothing (records were marked published).
	if err := relay.Dispatch(ctx); err != nil {
		t.Fatalf("dispatch 2: %v", err)
	}
	if len(bus.events) != 2 {
		t.Fatalf("no redelivery expected, got %d", len(bus.events))
	}
}

// TestOutboxRetriesOnBusFailure verifies at-least-once: if the bus rejects a
// delivery, the record stays unpublished and is retried on the next drain.
func TestOutboxRetriesOnBusFailure(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError + 1}))

	store := memory.NewOutboxStore()
	pub := outbox.NewPublisher(store)
	bus := &captureBus{failNext: true}
	relay := outbox.NewRelay(store, bus, log)

	_ = pub.Publish(ctx, stubEvent{"order.created", "o-2"})

	// First drain: bus fails -> nothing marked published.
	_ = relay.Dispatch(ctx)
	if len(bus.events) != 0 {
		t.Fatalf("delivery should have failed, got %d", len(bus.events))
	}
	// Second drain: bus succeeds -> event delivered (not lost).
	_ = relay.Dispatch(ctx)
	if len(bus.events) != 1 {
		t.Fatalf("event should be redelivered after failure, got %d", len(bus.events))
	}
}
