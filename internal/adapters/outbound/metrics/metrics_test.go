package metrics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	metricsadapter "github.com/example/go-ddd-hex/internal/adapters/outbound/metrics"
	order "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
	appmetrics "github.com/example/go-ddd-hex/internal/platform/metrics"
)

// ---------------------------------------------------------------------------
// Event publisher decorator.
// ---------------------------------------------------------------------------

type recordingPublisher struct {
	received []shared.Event
	err      error
}

func (p *recordingPublisher) Publish(_ context.Context, events ...shared.Event) error {
	if p.err != nil {
		return p.err
	}
	p.received = append(p.received, events...)
	return nil
}

type fakeEvent struct{ name string }

func (e fakeEvent) EventName() string   { return e.name }
func (fakeEvent) OccurredAt() time.Time { return time.Now().UTC() }

func TestEventPublisherCountsAndDelegates(t *testing.T) {
	m := appmetrics.New()
	inner := &recordingPublisher{}
	pub := metricsadapter.NewEventPublisher(inner, m)

	err := pub.Publish(context.Background(),
		fakeEvent{name: "order.created"},
		fakeEvent{name: "order.placed"},
		fakeEvent{name: "order.created"},
	)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Delegation is not optional: the decorator must not swallow the events.
	if len(inner.received) != 3 {
		t.Errorf("the inner publisher received %d events, want 3", len(inner.received))
	}
	if got := testutil.ToFloat64(m.DomainEvents.WithLabelValues("order.created")); got != 2 {
		t.Errorf("order.created: got %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.DomainEvents.WithLabelValues("order.placed")); got != 1 {
		t.Errorf("order.placed: got %v, want 1", got)
	}
}

func TestEventPublisherCountsDeliveriesNotAttempts(t *testing.T) {
	// The counter is named "dispatched", so a failed publish must not increment
	// it — otherwise the metric would claim events reached subscribers that
	// never saw them.
	m := appmetrics.New()
	boom := errors.New("the bus is down")
	pub := metricsadapter.NewEventPublisher(&recordingPublisher{err: boom}, m)

	if err := pub.Publish(context.Background(), fakeEvent{name: "order.created"}); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the inner error", err)
	}
	if got := testutil.ToFloat64(m.DomainEvents.WithLabelValues("order.created")); got != 0 {
		t.Errorf("a failed publish was counted: got %v, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Repository decorator.
// ---------------------------------------------------------------------------

// failingRepo makes Save fail, to separate a real fault from a normal miss.
type failingRepo struct {
	order.Repository
	saveErr error
}

func (r failingRepo) Save(ctx context.Context, o *order.Order) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.Repository.Save(ctx, o)
}

func draftOrder(t *testing.T) *order.Order {
	t.Helper()
	customerID, err := order.CustomerIDFromString(uuid.Must(uuid.NewV7()).String())
	if err != nil {
		t.Fatalf("CustomerIDFromString: %v", err)
	}
	o, err := order.NewDraft(customerID, "EUR")
	if err != nil {
		t.Fatalf("NewDraft: %v", err)
	}
	return o
}

func sampleCount(t *testing.T, m *appmetrics.Metrics, port, op string) uint64 {
	t.Helper()
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != "repository_operation_duration_seconds" {
			continue
		}
		for _, series := range f.GetMetric() {
			labels := map[string]string{}
			for _, pair := range series.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}
			if labels["port"] == port && labels["op"] == op {
				return series.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func TestRepositoryDecoratorMeasuresAndDelegates(t *testing.T) {
	ctx := context.Background()
	m := appmetrics.New()
	inner := memory.NewOrderRepository()
	repo := metricsadapter.NewOrderRepository(inner, m)

	o := draftOrder(t)
	if err := repo.Save(ctx, o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Delegation: the decorated repository must behave exactly like the one it
	// wraps, or it is not a transparent decorator.
	got, err := repo.FindByID(ctx, o.ID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !got.ID().Equals(o.ID()) {
		t.Errorf("id: got %s, want %s", got.ID(), o.ID())
	}

	if n := sampleCount(t, m, "order.Repository", "Save"); n != 1 {
		t.Errorf("Save observations: got %d, want 1", n)
	}
	if n := sampleCount(t, m, "order.Repository", "FindByID"); n != 1 {
		t.Errorf("FindByID observations: got %d, want 1", n)
	}
}

func TestRepositoryDecoratorDoesNotCountNotFoundAsAnError(t *testing.T) {
	// A lookup that finds nothing is the store working correctly. Counting it
	// would drown the real failures and make the metric useless for alerting.
	ctx := context.Background()
	m := appmetrics.New()
	repo := metricsadapter.NewOrderRepository(memory.NewOrderRepository(), m)

	if _, err := repo.FindByID(ctx, order.NewID()); !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	if got := testutil.ToFloat64(m.RepoErrors.WithLabelValues("order.Repository", "FindByID")); got != 0 {
		t.Errorf("ErrNotFound was counted as an error: got %v, want 0", got)
	}
	if n := sampleCount(t, m, "order.Repository", "FindByID"); n != 1 {
		t.Errorf("the call should still be timed: got %d observations, want 1", n)
	}
}

func TestRepositoryDecoratorCountsRealFailures(t *testing.T) {
	ctx := context.Background()
	m := appmetrics.New()
	boom := errors.New("database is unreachable")
	repo := metricsadapter.NewOrderRepository(
		failingRepo{Repository: memory.NewOrderRepository(), saveErr: boom}, m)

	// The inner error must come back untouched, so errors.Is upstream still
	// matches — wrapping it would break the application layer's classification.
	if err := repo.Save(ctx, draftOrder(t)); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the inner error", err)
	}
	if got := testutil.ToFloat64(m.RepoErrors.WithLabelValues("order.Repository", "Save")); got != 1 {
		t.Errorf("failures counted: got %v, want 1", got)
	}
}

func TestRepositoryDecoratorCoversEveryOperation(t *testing.T) {
	// A decorator that forgets a method silently loses that method's metrics,
	// and nothing else would notice.
	ctx := context.Background()
	m := appmetrics.New()
	repo := metricsadapter.NewOrderRepository(memory.NewOrderRepository(), m)

	o := draftOrder(t)
	if err := repo.Save(ctx, o); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := repo.FindByID(ctx, o.ID()); err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if _, err := repo.List(ctx, 10, 0); err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := repo.ListByCustomer(ctx, o.CustomerID()); err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if err := repo.Delete(ctx, o.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	for _, op := range []string{"Save", "FindByID", "List", "ListByCustomer", "Delete"} {
		if n := sampleCount(t, m, "order.Repository", op); n == 0 {
			t.Errorf("%s is not instrumented", op)
		}
	}
}
