package outbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// envelope is the shared.Event delivered to the bus when a stored record is
// relayed. It carries the event name, aggregate id and raw payload. It also
// implements shared.Keyed so the projector can find the aggregate id without
// knowing the concrete event type.
type envelope struct {
	name        string
	aggregateID string
	occurredAt  time.Time
	payload     []byte
}

func (e envelope) EventName() string     { return e.name }
func (e envelope) OccurredAt() time.Time { return e.occurredAt }
func (e envelope) AggregateID() string   { return e.aggregateID }

// Payload returns the raw stored JSON, for consumers that need it.
func (e envelope) Payload() []byte { return e.payload }

var _ shared.Event = envelope{}
var _ shared.Keyed = envelope{}

// Relay forwards stored outbox records to the real event bus. It provides
// at-least-once delivery: a record is only marked published after the bus
// accepts it, so a crash mid-dispatch causes a redelivery (consumers must be
// idempotent), never a loss.
type Relay struct {
	store     shared.OutboxStore
	bus       shared.EventPublisher
	log       *slog.Logger
	batchSize int

	mu   sync.Mutex // serializes Dispatch (inline trigger vs. ticker)
	stop chan struct{}
}

// NewRelay builds a relay that drains store into bus.
func NewRelay(store shared.OutboxStore, bus shared.EventPublisher, log *slog.Logger) *Relay {
	return &Relay{store: store, bus: bus, log: log, batchSize: 100}
}

// Dispatch drains one batch of unpublished records to the bus and marks the
// successfully delivered ones. It is safe to call concurrently (it serializes
// internally) and is idempotent at the record level. Implements the
// order.Dispatcher hint used for low-latency inline delivery.
func (r *Relay) Dispatch(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	records, err := r.store.FetchUnpublished(ctx, r.batchSize)
	if err != nil {
		return err
	}
	delivered := make([]string, 0, len(records))
	for _, rec := range records {
		env := envelope{name: rec.EventName, aggregateID: rec.AggregateID, occurredAt: rec.OccurredAt, payload: rec.Payload}
		if err := r.bus.Publish(ctx, env); err != nil {
			// Leave the record unpublished; it will be retried next tick.
			r.log.ErrorContext(ctx, "relay publish failed, will retry",
				slog.String("event", rec.EventName), slog.Any("error", err))
			continue
		}
		delivered = append(delivered, rec.ID)
	}
	if len(delivered) == 0 {
		return nil
	}
	return r.store.MarkPublished(ctx, delivered...)
}

// Start launches the background poller (the durability backstop). It keeps
// draining on an interval until Stop is called, so any record left behind by a
// crash between commit and an inline Dispatch is eventually delivered.
func (r *Relay) Start(ctx context.Context, interval time.Duration) {
	r.stop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.Dispatch(ctx); err != nil {
					r.log.ErrorContext(ctx, "relay tick failed", slog.Any("error", err))
				}
			}
		}
	}()
}

// Stop halts the background poller.
func (r *Relay) Stop() {
	if r.stop != nil {
		close(r.stop)
		r.stop = nil
	}
}
