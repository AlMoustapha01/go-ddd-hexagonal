// Package shared is the SHARED KERNEL: a small set of concepts that several
// aggregates and bounded contexts agree on. Keeping it deliberately tiny avoids
// coupling aggregates together. Here it holds the domain-event abstraction that
// lets an in-process event bus dispatch events from any aggregate.
package shared

import (
	"context"
	"time"
)

// Event is any domain event. Every aggregate defines its own concrete events
// (user.Registered, order.Placed, ...) but they all satisfy this interface so a
// single publisher/bus can carry them.
type Event interface {
	EventName() string
	OccurredAt() time.Time
}

// EventPublisher is the OUTBOUND PORT for dispatching domain events. Adapters
// implement it (in-process bus, Kafka, NATS, a no-op, ...).
type EventPublisher interface {
	Publish(ctx context.Context, events ...Event) error
}

// BaseEvent is embedded by concrete events to carry the occurrence timestamp.
type BaseEvent struct {
	At time.Time
}

// OccurredAt implements part of Event.
func (b BaseEvent) OccurredAt() time.Time { return b.At }

// EventAt builds the base of an event that occurred at the given instant.
//
// The timestamp is a parameter rather than an internal time.Now() call on
// purpose: an aggregate that has just touched its own updatedAt passes that same
// value, so the state change and the event it emits are stamped identically
// instead of a few nanoseconds apart.
func EventAt(at time.Time) BaseEvent { return BaseEvent{At: at} }
