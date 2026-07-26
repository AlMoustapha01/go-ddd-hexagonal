// Package eventbus is an OUTBOUND ADAPTER implementing shared.EventPublisher as
// a simple synchronous, in-process event bus. Handlers subscribe by event name;
// Publish logs each event and fans it out to its subscribers.
//
// This is how aggregates stay decoupled yet coordinated: the User aggregate
// emits user.deactivated without knowing anything about Orders, and an
// Order-side handler subscribes to react. Swap this for Kafka/NATS later and
// nothing in the domain or application layers changes.
package eventbus

import (
	"context"
	"log/slog"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// HandlerFunc reacts to a single domain event.
type HandlerFunc func(ctx context.Context, e shared.Event) error

// Bus is a synchronous in-process dispatcher.
type Bus struct {
	handlers map[string][]HandlerFunc
	log      *slog.Logger
}

func New(log *slog.Logger) *Bus {
	return &Bus{handlers: make(map[string][]HandlerFunc), log: log}
}

var _ shared.EventPublisher = (*Bus)(nil)

// Subscribe registers a handler for a given event name. Call it at wiring time
// (composition root), before the app starts serving.
func (b *Bus) Subscribe(eventName string, h HandlerFunc) {
	b.handlers[eventName] = append(b.handlers[eventName], h)
}

// Publish logs and dispatches events to their subscribers. A handler failure is
// logged but does not abort the others, matching eventual-consistency
// semantics: the aggregate that emitted the event is already persisted.
func (b *Bus) Publish(ctx context.Context, events ...shared.Event) error {
	for _, e := range events {
		b.log.InfoContext(ctx, "domain event",
			slog.String("event", e.EventName()),
			slog.Time("occurred_at", e.OccurredAt()),
		)
		for _, h := range b.handlers[e.EventName()] {
			if err := h(ctx, e); err != nil {
				b.log.ErrorContext(ctx, "event handler failed",
					slog.String("event", e.EventName()),
					slog.Any("error", err),
				)
			}
		}
	}
	return nil
}
