// Package metrics holds OUTBOUND ADAPTERS that decorate other outbound
// adapters with instrumentation.
//
// This is the payoff of ports and adapters: a cross-cutting concern is added by
// WRAPPING a port, so not one line of domain or application code changes or
// even knows it is being measured. The composition root decides whether the
// wrapping happens at all.
package metrics

import (
	"context"

	"github.com/example/go-ddd-hex/internal/domain/shared"
	appmetrics "github.com/example/go-ddd-hex/internal/platform/metrics"
)

// EventPublisher counts domain events, then delegates.
//
// Wrap the BUS with this, and only the bus. Wrapping the outbox publisher too
// would double-count every order event: once when it is written to the outbox
// table, once when the relay forwards it to the bus. Counting at the bus yields
// exactly one increment per event actually dispatched.
type EventPublisher struct {
	inner   shared.EventPublisher
	metrics *appmetrics.Metrics
}

// NewEventPublisher decorates an event publisher with a domain-event counter.
func NewEventPublisher(inner shared.EventPublisher, m *appmetrics.Metrics) *EventPublisher {
	return &EventPublisher{inner: inner, metrics: m}
}

var _ shared.EventPublisher = (*EventPublisher)(nil)

// Publish delegates first and counts only what was actually dispatched, so the
// counter measures deliveries rather than attempts.
func (p *EventPublisher) Publish(ctx context.Context, events ...shared.Event) error {
	if err := p.inner.Publish(ctx, events...); err != nil {
		return err
	}
	for _, e := range events {
		p.metrics.DomainEvents.WithLabelValues(e.EventName()).Inc()
	}
	return nil
}
