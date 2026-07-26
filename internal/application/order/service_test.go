package order_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	app "github.com/example/go-ddd-hex/internal/application/order"
	domain "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// ---------------------------------------------------------------------------
// Doubles.
//
// The repository is the real in-memory adapter — contract-tested against the
// same suite as Postgres, so it is a proven implementation rather than a
// fiction. It is wrapped only to observe WHERE each call happened and to inject
// failures a correct adapter would never produce.
// ---------------------------------------------------------------------------

// recordingUoW is a shared.Atomic that knows whether it is currently running a
// callback, so collaborators can report whether they were called inside the
// transaction or outside it.
type recordingUoW struct {
	inside     bool
	committed  int
	rolledBack int
}

func (u *recordingUoW) Within(ctx context.Context, fn func(context.Context) error) error {
	u.inside = true
	err := fn(ctx)
	u.inside = false
	if err != nil {
		u.rolledBack++
		return err
	}
	u.committed++
	return nil
}

// txAwareRepo records, for every Save, whether a transaction was open.
type txAwareRepo struct {
	domain.Repository
	uow       *recordingUoW
	savedInTx []bool
	saveErr   error
}

func (r *txAwareRepo) Save(ctx context.Context, o *domain.Order) error {
	r.savedInTx = append(r.savedInTx, r.uow.inside)
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.Repository.Save(ctx, o)
}

// txAwareOutbox records the same thing for every Publish.
type txAwareOutbox struct {
	uow           *recordingUoW
	publishedInTx []bool
	events        []shared.Event
	err           error
}

func (o *txAwareOutbox) Publish(_ context.Context, events ...shared.Event) error {
	o.publishedInTx = append(o.publishedInTx, o.uow.inside)
	if o.err != nil {
		return o.err
	}
	o.events = append(o.events, events...)
	return nil
}

func (o *txAwareOutbox) names() []string {
	out := make([]string, 0, len(o.events))
	for _, e := range o.events {
		out = append(out, e.EventName())
	}
	return out
}

// stubChecker answers the cross-aggregate customer question.
type stubChecker struct {
	active bool
	err    error
}

func (c stubChecker) IsActiveCustomer(context.Context, string) (bool, error) {
	return c.active, c.err
}

// stubDispatcher counts the post-commit relay hints and can fail.
type stubDispatcher struct {
	calls int
	err   error
}

func (d *stubDispatcher) Dispatch(context.Context) error {
	d.calls++
	return d.err
}

// harness bundles a wired service with everything the test needs to inspect.
type harness struct {
	svc        *app.Service
	repo       *txAwareRepo
	outbox     *txAwareOutbox
	uow        *recordingUoW
	dispatcher *stubDispatcher
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	uow := &recordingUoW{}
	h := &harness{
		repo:       &txAwareRepo{Repository: memory.NewOrderRepository(), uow: uow},
		outbox:     &txAwareOutbox{uow: uow},
		uow:        uow,
		dispatcher: &stubDispatcher{},
	}
	h.svc = app.NewService(h.repo, stubChecker{active: true}, uow, h.outbox, h.dispatcher, discardLogger())
	return h
}

func newCustomerID() string { return uuid.Must(uuid.NewV7()).String() }

// draft creates an order through the service.
func (h *harness) draft(t *testing.T) app.OrderView {
	t.Helper()
	v, err := h.svc.Create(context.Background(), app.CreateCommand{CustomerID: newCustomerID(), Currency: "EUR"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return v
}

// ---------------------------------------------------------------------------
// The Outbox contract, at this layer.
//
// Whether the transaction is REAL is the adapter's job, and is proven against a
// live PostgreSQL by TestUnitOfWorkRollsBackAggregateAndOutboxTogether. What
// the application layer owes is that it CALLS Within and puts both writes
// inside it. Neither test is sufficient alone; together they cover the pattern.
// ---------------------------------------------------------------------------

func TestAggregateAndEventsAreWrittenInsideOneTransaction(t *testing.T) {
	h := newHarness(t)

	h.draft(t)

	if len(h.repo.savedInTx) != 1 {
		t.Fatalf("Save calls: got %d, want 1", len(h.repo.savedInTx))
	}
	if !h.repo.savedInTx[0] {
		t.Error("the aggregate was saved OUTSIDE the unit of work")
	}
	if len(h.outbox.publishedInTx) != 1 {
		t.Fatalf("Publish calls: got %d, want 1", len(h.outbox.publishedInTx))
	}
	if !h.outbox.publishedInTx[0] {
		t.Error("the events were published OUTSIDE the unit of work — that is the dual-write hazard")
	}
	if h.uow.committed != 1 || h.uow.rolledBack != 0 {
		t.Errorf("unit of work: %d committed, %d rolled back; want 1 and 0", h.uow.committed, h.uow.rolledBack)
	}
	if names := h.outbox.names(); len(names) != 1 || names[0] != "order.created" {
		t.Errorf("events: got %v, want [order.created]", names)
	}
}

func TestAPublishFailureAbortsTheTransaction(t *testing.T) {
	h := newHarness(t)
	boom := errors.New("outbox insert failed")
	h.outbox.err = boom

	_, err := h.svc.Create(context.Background(), app.CreateCommand{CustomerID: newCustomerID(), Currency: "EUR"})

	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the outbox error", err)
	}
	if h.uow.committed != 0 {
		t.Error("the unit of work must not commit when the outbox write fails")
	}
	if h.uow.rolledBack != 1 {
		t.Errorf("rolled back: got %d, want 1", h.uow.rolledBack)
	}
	if h.dispatcher.calls != 0 {
		t.Error("no relay hint may be sent for a transaction that never committed")
	}
}

func TestASaveFailureAbortsBeforePublishing(t *testing.T) {
	h := newHarness(t)
	boom := errors.New("disk is full")
	h.repo.saveErr = boom

	_, err := h.svc.Create(context.Background(), app.CreateCommand{CustomerID: newCustomerID(), Currency: "EUR"})

	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want the repository error", err)
	}
	if len(h.outbox.publishedInTx) != 0 {
		t.Error("events must not be published when the aggregate failed to save")
	}
	if h.uow.committed != 0 {
		t.Error("the unit of work must not commit")
	}
}

func TestTheInlineRelayHintIsBestEffort(t *testing.T) {
	// The hint exists only to cut latency; the relay's poller is the durability
	// backstop. A failing hint must therefore not fail an already-committed
	// command — doing so would report failure for a change that did happen.
	h := newHarness(t)
	h.dispatcher.err = errors.New("relay is busy")

	got, err := h.svc.Create(context.Background(), app.CreateCommand{CustomerID: newCustomerID(), Currency: "EUR"})
	if err != nil {
		t.Fatalf("the command must succeed despite the relay hint failing: %v", err)
	}
	if got.ID == "" {
		t.Error("a view should still be returned")
	}
	if h.uow.committed != 1 {
		t.Error("the transaction should have committed")
	}
	if h.dispatcher.calls != 1 {
		t.Errorf("dispatch calls: got %d, want 1", h.dispatcher.calls)
	}
}

func TestTheDispatcherIsOptional(t *testing.T) {
	uow := &recordingUoW{}
	repo := &txAwareRepo{Repository: memory.NewOrderRepository(), uow: uow}
	out := &txAwareOutbox{uow: uow}
	svc := app.NewService(repo, stubChecker{active: true}, uow, out, nil, discardLogger())

	if _, err := svc.Create(context.Background(), app.CreateCommand{CustomerID: newCustomerID(), Currency: "EUR"}); err != nil {
		t.Fatalf("Create with no dispatcher wired: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The cross-aggregate customer rule.
// ---------------------------------------------------------------------------

func TestCreateEnforcesTheCustomerRule(t *testing.T) {
	ctx := context.Background()
	cmd := app.CreateCommand{CustomerID: newCustomerID(), Currency: "EUR"}

	t.Run("rejects an inactive or unknown customer", func(t *testing.T) {
		uow := &recordingUoW{}
		repo := &txAwareRepo{Repository: memory.NewOrderRepository(), uow: uow}
		svc := app.NewService(repo, stubChecker{active: false}, uow, &txAwareOutbox{uow: uow}, nil, discardLogger())

		if _, err := svc.Create(ctx, cmd); !errors.Is(err, app.ErrCustomerNotActive) {
			t.Fatalf("got %v, want ErrCustomerNotActive", err)
		}
		if len(repo.savedInTx) != 0 {
			t.Error("nothing should have been written")
		}
	})

	t.Run("propagates a failure of the customer check", func(t *testing.T) {
		boom := errors.New("user service is down")
		uow := &recordingUoW{}
		svc := app.NewService(
			&txAwareRepo{Repository: memory.NewOrderRepository(), uow: uow},
			stubChecker{err: boom}, uow, &txAwareOutbox{uow: uow}, nil, discardLogger())

		if _, err := svc.Create(ctx, cmd); !errors.Is(err, boom) {
			t.Fatalf("got %v, want the checker error", err)
		}
	})

	t.Run("rejects a malformed customer id", func(t *testing.T) {
		h := newHarness(t)
		_, err := h.svc.Create(ctx, app.CreateCommand{CustomerID: "not-a-uuid", Currency: "EUR"})
		if !errors.Is(err, domain.ErrInvalidCustomer) {
			t.Fatalf("got %v, want ErrInvalidCustomer", err)
		}
	})

	t.Run("rejects a malformed currency", func(t *testing.T) {
		h := newHarness(t)
		_, err := h.svc.Create(ctx, app.CreateCommand{CustomerID: newCustomerID(), Currency: "EUROS"})
		if !errors.Is(err, domain.ErrInvalidCurrency) {
			t.Fatalf("got %v, want ErrInvalidCurrency", err)
		}
	})
}

// ---------------------------------------------------------------------------
// The lifecycle, through the use cases.
// ---------------------------------------------------------------------------

func TestOrderLifecycleEmitsOneEventPerTransition(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	created := h.draft(t)

	if _, err := h.svc.AddLine(ctx, app.AddLineCommand{
		OrderID: created.ID, ProductID: "sku-1", UnitPriceMinor: 1050, Currency: "EUR", Quantity: 2,
	}); err != nil {
		t.Fatalf("AddLine: %v", err)
	}

	placed, err := h.svc.Place(ctx, created.ID)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if placed.TotalMinor != 2100 {
		t.Errorf("total: got %d, want %d", placed.TotalMinor, 2100)
	}

	paid, err := h.svc.Pay(ctx, created.ID)
	if err != nil {
		t.Fatalf("Pay: %v", err)
	}
	if paid.Status != "paid" {
		t.Errorf("status: got %q, want %q", paid.Status, "paid")
	}

	want := []string{"order.created", "order.line_added", "order.placed", "order.paid"}
	got := h.outbox.names()
	if len(got) != len(want) {
		t.Fatalf("events: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d: got %q, want %q", i, got[i], want[i])
		}
	}

	// Every one of those transitions must have gone through the unit of work.
	if h.uow.committed != len(want) {
		t.Errorf("commits: got %d, want %d", h.uow.committed, len(want))
	}
	for i, inTx := range h.repo.savedInTx {
		if !inTx {
			t.Errorf("save %d happened outside the unit of work", i)
		}
	}
}

func TestCancel(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	created := h.draft(t)

	got, err := h.svc.Cancel(ctx, created.ID, "customer changed their mind")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status: got %q, want %q", got.Status, "cancelled")
	}
	if _, err := h.svc.Cancel(ctx, created.ID, "again"); !errors.Is(err, domain.ErrAlreadyFinalized) {
		t.Errorf("cancelling twice: got %v, want ErrAlreadyFinalized", err)
	}
}

func TestDomainInvariantsSurfaceThroughTheUseCases(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	created := h.draft(t)

	if _, err := h.svc.Place(ctx, created.ID); !errors.Is(err, domain.ErrNoLines) {
		t.Errorf("placing an empty order: got %v, want ErrNoLines", err)
	}
	if _, err := h.svc.Pay(ctx, created.ID); !errors.Is(err, domain.ErrNotPlaced) {
		t.Errorf("paying a draft: got %v, want ErrNotPlaced", err)
	}
	if _, err := h.svc.AddLine(ctx, app.AddLineCommand{
		OrderID: created.ID, ProductID: "sku-1", UnitPriceMinor: 100, Currency: "USD", Quantity: 1,
	}); !errors.Is(err, domain.ErrLineCurrency) {
		t.Errorf("mixing currencies: got %v, want ErrLineCurrency", err)
	}
	if _, err := h.svc.AddLine(ctx, app.AddLineCommand{
		OrderID: created.ID, ProductID: "sku-1", UnitPriceMinor: -1, Currency: "EUR", Quantity: 1,
	}); !errors.Is(err, domain.ErrNegativeAmount) {
		t.Errorf("negative price: got %v, want ErrNegativeAmount", err)
	}
}

func TestMutateOnMalformedOrUnknownID(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	for name, id := range map[string]string{
		"malformed id": "not-a-uuid",
		"unknown id":   domain.NewID().String(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.svc.Place(ctx, id); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("Place: got %v, want ErrNotFound", err)
			}
			if _, err := h.svc.Cancel(ctx, id, "reason"); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("Cancel: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestIsValidationError(t *testing.T) {
	// The HTTP layer consults this to choose between 400 and 500, so a missing
	// entry silently turns a client mistake into a server error.
	validation := []error{
		domain.ErrInvalidCurrency,
		domain.ErrNegativeAmount,
		domain.ErrCurrencyMismatch,
		domain.ErrEmptyProduct,
		domain.ErrInvalidQuantity,
		domain.ErrNoLines,
		domain.ErrNotDraft,
		domain.ErrNotPlaced,
		domain.ErrAlreadyFinalized,
		domain.ErrLineCurrency,
		domain.ErrInvalidCustomer,
		app.ErrCustomerNotActive,
	}
	for _, err := range validation {
		if !app.IsValidationError(err) {
			t.Errorf("%v should be classed as a validation error", err)
		}
	}

	for _, err := range []error{domain.ErrNotFound, errors.New("database is unreachable"), nil} {
		if app.IsValidationError(err) {
			t.Errorf("%v should NOT be classed as a validation error", err)
		}
	}
}
