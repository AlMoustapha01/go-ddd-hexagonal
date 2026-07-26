package order_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	app "github.com/example/go-ddd-hex/internal/application/order"
	domain "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// keyedEvent stands in for anything the projector may receive off the bus.
//
// It is deliberately NOT a concrete order event: in production the projector is
// usually fed an opaque envelope replayed by the outbox relay, which satisfies
// shared.Keyed and nothing more. Testing against that shape is testing what
// actually arrives.
type keyedEvent struct {
	name string
	id   string
}

func (e keyedEvent) EventName() string   { return e.name }
func (keyedEvent) OccurredAt() time.Time { return time.Now().UTC() }
func (e keyedEvent) AggregateID() string { return e.id }

// unkeyedEvent is an event from another context — a user event, say — which
// does not implement shared.Keyed.
type unkeyedEvent struct{}

func (unkeyedEvent) EventName() string     { return "user.registered" }
func (unkeyedEvent) OccurredAt() time.Time { return time.Now().UTC() }

// failingReadStore fails on demand, to reach the error branch.
type failingReadStore struct {
	app.ReadStore
	upsertErr error
}

func (s failingReadStore) Upsert(ctx context.Context, d app.DetailView) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	return s.ReadStore.Upsert(ctx, d)
}

// seedOrder puts a real order in the write repository and returns its id.
func seedOrder(t *testing.T, repo domain.Repository) string {
	t.Helper()
	customerID, err := domain.CustomerIDFromString(newCustomerID())
	if err != nil {
		t.Fatalf("CustomerIDFromString: %v", err)
	}
	o, err := domain.NewDraft(customerID, "EUR")
	if err != nil {
		t.Fatalf("NewDraft: %v", err)
	}
	price, err := domain.NewMoney(1050, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	if err := o.AddLine("sku-1", price, 2); err != nil {
		t.Fatalf("AddLine: %v", err)
	}
	if err := repo.Save(context.Background(), o); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return o.ID().String()
}

func TestProjectorRebuildsTheReadModel(t *testing.T) {
	ctx := context.Background()
	writeRepo := memory.NewOrderRepository()
	read := memory.NewOrderReadStore()
	p := app.NewProjector(writeRepo, read, discardLogger())

	id := seedOrder(t, writeRepo)

	if err := p.Handle(ctx, keyedEvent{name: "order.created", id: id}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	got, err := read.Get(ctx, id)
	if err != nil {
		t.Fatalf("the projection should exist: %v", err)
	}
	if got.ID != id {
		t.Errorf("id: got %q, want %q", got.ID, id)
	}
	if got.Status != "draft" {
		t.Errorf("status: got %q, want %q", got.Status, "draft")
	}
	if got.TotalMinor != 2100 {
		t.Errorf("total: got %d, want %d", got.TotalMinor, 2100)
	}
	if got.ItemCount != 1 {
		t.Errorf("itemCount: got %d, want %d", got.ItemCount, 1)
	}
	if len(got.Lines) != 1 || got.Lines[0].ProductID != "sku-1" {
		t.Errorf("lines: got %+v", got.Lines)
	}
}

func TestProjectorDropsTheProjectionWhenTheAggregateIsGone(t *testing.T) {
	// The read model must not outlive the write model, or a deleted order stays
	// queryable forever.
	ctx := context.Background()
	writeRepo := memory.NewOrderRepository()
	read := memory.NewOrderReadStore()
	p := app.NewProjector(writeRepo, read, discardLogger())

	id := seedOrder(t, writeRepo)
	if err := p.Handle(ctx, keyedEvent{name: "order.created", id: id}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	oid, err := domain.IDFromString(id)
	if err != nil {
		t.Fatalf("IDFromString: %v", err)
	}
	if err := writeRepo.Delete(ctx, oid); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if err := p.Handle(ctx, keyedEvent{name: "order.cancelled", id: id}); err != nil {
		t.Fatalf("Handle after delete: %v", err)
	}
	if _, err := read.Get(ctx, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the projection should have been dropped: got %v, want ErrNotFound", err)
	}
}

func TestProjectorIgnoresWhatIsNotItsBusiness(t *testing.T) {
	// The projector is subscribed by event name, but the bus and the relay may
	// hand it anything. It must be inert rather than error, or one foreign event
	// would break the whole subscription.
	ctx := context.Background()
	read := memory.NewOrderReadStore()
	p := app.NewProjector(memory.NewOrderRepository(), read, discardLogger())

	t.Run("an event without an aggregate id", func(t *testing.T) {
		if err := p.Handle(ctx, unkeyedEvent{}); err != nil {
			t.Errorf("got %v, want nil", err)
		}
	})

	t.Run("an aggregate id that is not an order id", func(t *testing.T) {
		if err := p.Handle(ctx, keyedEvent{name: "other.happened", id: "not-a-uuid"}); err != nil {
			t.Errorf("got %v, want nil", err)
		}
	})

	t.Run("an order that does not exist and has no projection", func(t *testing.T) {
		if err := p.Handle(ctx, keyedEvent{name: "order.created", id: domain.NewID().String()}); err != nil {
			t.Errorf("got %v, want nil", err)
		}
	})
}

func TestProjectorPropagatesAReadStoreFailure(t *testing.T) {
	// Here the error MUST surface: the outbox relay retries on failure, and
	// swallowing it would leave the read model permanently stale.
	ctx := context.Background()
	writeRepo := memory.NewOrderRepository()
	boom := errors.New("read store is unreachable")
	read := failingReadStore{ReadStore: memory.NewOrderReadStore(), upsertErr: boom}
	p := app.NewProjector(writeRepo, read, discardLogger())

	id := seedOrder(t, writeRepo)

	if err := p.Handle(ctx, keyedEvent{name: "order.created", id: id}); !errors.Is(err, boom) {
		t.Fatalf("got %v, want the read store error", err)
	}
}

func TestProjectorSubscribesToEveryEventTheServiceEmits(t *testing.T) {
	// The subscription list and the events actually emitted are declared in two
	// different files. If they drift — a new transition whose event nobody
	// subscribed to — the read model silently stops tracking it. This walks the
	// full lifecycle and checks every emitted name is covered.
	ctx := context.Background()
	h := newHarness(t)
	created := h.draft(t)

	if _, err := h.svc.AddLine(ctx, app.AddLineCommand{
		OrderID: created.ID, ProductID: "sku-1", UnitPriceMinor: 100, Currency: "EUR", Quantity: 1,
	}); err != nil {
		t.Fatalf("AddLine: %v", err)
	}
	if _, err := h.svc.Place(ctx, created.ID); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if _, err := h.svc.Pay(ctx, created.ID); err != nil {
		t.Fatalf("Pay: %v", err)
	}

	// Cancel needs a separate order: a paid one is final.
	other := h.draft(t)
	if _, err := h.svc.Cancel(ctx, other.ID, "changed their mind"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	subscribed := make(map[string]bool)
	for _, name := range app.NewProjector(nil, nil, discardLogger()).EventNames() {
		subscribed[name] = true
	}
	for _, emitted := range h.outbox.names() {
		if !subscribed[emitted] {
			t.Errorf("the service emits %q but the projector does not subscribe to it", emitted)
		}
	}
}

var _ shared.Keyed = keyedEvent{}
