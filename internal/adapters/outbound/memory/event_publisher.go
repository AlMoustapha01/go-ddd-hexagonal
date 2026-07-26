package memory

import (
	"context"
	"log/slog"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// LoggingEventPublisher is a trivial shared.EventPublisher that only logs. It
// is handy in tests where you don't need real subscribers. In production the
// eventbus.Bus is used instead (it logs AND dispatches to handlers).
type LoggingEventPublisher struct {
	log *slog.Logger
}

func NewLoggingEventPublisher(log *slog.Logger) *LoggingEventPublisher {
	return &LoggingEventPublisher{log: log}
}

var _ shared.EventPublisher = (*LoggingEventPublisher)(nil)

func (p *LoggingEventPublisher) Publish(ctx context.Context, events ...shared.Event) error {
	for _, e := range events {
		p.log.InfoContext(ctx, "domain event",
			slog.String("event", e.EventName()),
			slog.Time("occurred_at", e.OccurredAt()),
		)
	}
	return nil
}
