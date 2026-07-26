package integration_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/eventbus"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	"github.com/example/go-ddd-hex/internal/application/integration"
	orderapp "github.com/example/go-ddd-hex/internal/application/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
	user "github.com/example/go-ddd-hex/internal/domain/user"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// ---------------------------------------------------------------------------
// The anti-corruption layer: order asks "is this an active customer?" through a
// narrow port, and never learns the shape of the user aggregate.
// ---------------------------------------------------------------------------

// failingUserRepo makes FindByID fail, to separate "no such customer" from
// "the lookup itself broke" — a distinction that decides between a 400 and a
// 500 upstream.
type failingUserRepo struct {
	user.Repository
	err error
}

func (r failingUserRepo) FindByID(ctx context.Context, id user.ID) (*user.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.Repository.FindByID(ctx, id)
}

func saveUser(t *testing.T, repo user.Repository, active bool) user.ID {
	t.Helper()
	email, err := user.NewEmail("alice@example.com")
	if err != nil {
		t.Fatalf("NewEmail: %v", err)
	}
	u, err := user.Register("Alice", email)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if active {
		if err := u.Activate(); err != nil {
			t.Fatalf("Activate: %v", err)
		}
	}
	if err := repo.Save(context.Background(), u); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return u.ID()
}

func TestIsActiveCustomer(t *testing.T) {
	ctx := context.Background()

	t.Run("reports true for an active user", func(t *testing.T) {
		repo := memory.NewUserRepository()
		id := saveUser(t, repo, true)

		got, err := integration.NewUserCustomerChecker(repo).IsActiveCustomer(ctx, id.String())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Error("got false, want true")
		}
	})

	t.Run("reports false for an inactive user", func(t *testing.T) {
		repo := memory.NewUserRepository()
		id := saveUser(t, repo, false)

		got, err := integration.NewUserCustomerChecker(repo).IsActiveCustomer(ctx, id.String())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Error("got true, want false")
		}
	})

	t.Run("a malformed id is not a customer, and not an error", func(t *testing.T) {
		// A bad id in a request body is a client mistake, not an outage. Turning
		// it into an error here would surface as a 500 instead of a 400.
		got, err := integration.NewUserCustomerChecker(memory.NewUserRepository()).
			IsActiveCustomer(ctx, "not-a-uuid")
		if err != nil {
			t.Fatalf("got error %v, want nil", err)
		}
		if got {
			t.Error("got true, want false")
		}
	})

	t.Run("an unknown user is not a customer, and not an error", func(t *testing.T) {
		got, err := integration.NewUserCustomerChecker(memory.NewUserRepository()).
			IsActiveCustomer(ctx, user.NewID().String())
		if err != nil {
			t.Fatalf("got error %v, want nil", err)
		}
		if got {
			t.Error("got true, want false")
		}
	})

	t.Run("a broken lookup IS an error", func(t *testing.T) {
		// The opposite case: infrastructure failure must not be quietly reported
		// as "this customer is not active", which would reject valid orders
		// during an outage.
		boom := errors.New("database is unreachable")
		repo := failingUserRepo{Repository: memory.NewUserRepository(), err: boom}

		if _, err := integration.NewUserCustomerChecker(repo).IsActiveCustomer(ctx, user.NewID().String()); !errors.Is(err, boom) {
			t.Fatalf("got %v, want the repository error", err)
		}
	})
}

// ---------------------------------------------------------------------------
// The cross-context reaction: deactivating a user cancels their open orders.
// ---------------------------------------------------------------------------

// stubQueries serves the read side of the order context.
type stubQueries struct {
	orderapp.Queries
	summaries []orderapp.SummaryView
	err       error
}

func (q stubQueries) ListCustomerOrders(context.Context, string) ([]orderapp.SummaryView, error) {
	return q.summaries, q.err
}

// recordingCommands captures which orders the handler tried to cancel.
type recordingCommands struct {
	orderapp.UseCases
	cancelled []string
	failOn    map[string]error
}

func (c *recordingCommands) Cancel(_ context.Context, id, _ string) (orderapp.OrderView, error) {
	if err, ok := c.failOn[id]; ok {
		return orderapp.OrderView{}, err
	}
	c.cancelled = append(c.cancelled, id)
	return orderapp.OrderView{ID: id, Status: "cancelled"}, nil
}

func deactivated(id user.ID) user.Deactivated {
	return user.Deactivated{BaseEvent: shared.EventAt(time.Now().UTC()), UserID: id}
}

func TestCancelOrdersOnUserDeactivation(t *testing.T) {
	ctx := context.Background()

	t.Run("cancels only the orders still open", func(t *testing.T) {
		// A paid or already-cancelled order must be left alone: cancelling a
		// paid order would contradict a completed transaction.
		queries := stubQueries{summaries: []orderapp.SummaryView{
			{ID: "order-draft", Status: "draft"},
			{ID: "order-placed", Status: "placed"},
			{ID: "order-paid", Status: "paid"},
			{ID: "order-cancelled", Status: "cancelled"},
		}}
		commands := &recordingCommands{}

		h := integration.NewCancelOrdersOnUserDeactivation(commands, queries, discardLogger())
		if err := h.Handle(ctx, deactivated(user.NewID())); err != nil {
			t.Fatalf("Handle: %v", err)
		}

		want := []string{"order-draft", "order-placed"}
		if len(commands.cancelled) != len(want) {
			t.Fatalf("cancelled: got %v, want %v", commands.cancelled, want)
		}
		for i := range want {
			if commands.cancelled[i] != want[i] {
				t.Errorf("cancelled %d: got %q, want %q", i, commands.cancelled[i], want[i])
			}
		}
	})

	t.Run("carries on when one cancellation fails", func(t *testing.T) {
		// Eventual consistency: the user change is already committed. Aborting
		// on the first failure would leave the remaining orders open forever.
		queries := stubQueries{summaries: []orderapp.SummaryView{
			{ID: "order-1", Status: "draft"},
			{ID: "order-2", Status: "draft"},
			{ID: "order-3", Status: "placed"},
		}}
		commands := &recordingCommands{failOn: map[string]error{"order-2": errors.New("conflict")}}

		h := integration.NewCancelOrdersOnUserDeactivation(commands, queries, discardLogger())
		if err := h.Handle(ctx, deactivated(user.NewID())); err != nil {
			t.Fatalf("Handle should not fail because one order did: %v", err)
		}

		want := []string{"order-1", "order-3"}
		if len(commands.cancelled) != len(want) {
			t.Fatalf("cancelled: got %v, want %v", commands.cancelled, want)
		}
	})

	t.Run("ignores an event it does not care about", func(t *testing.T) {
		commands := &recordingCommands{}
		h := integration.NewCancelOrdersOnUserDeactivation(commands, stubQueries{}, discardLogger())

		if err := h.Handle(ctx, user.Activated{BaseEvent: shared.EventAt(time.Now().UTC()), UserID: user.NewID()}); err != nil {
			t.Fatalf("got %v, want nil", err)
		}
		if len(commands.cancelled) != 0 {
			t.Errorf("nothing should have been cancelled, got %v", commands.cancelled)
		}
	})

	t.Run("propagates a failure to read the orders", func(t *testing.T) {
		boom := errors.New("read store is unreachable")
		h := integration.NewCancelOrdersOnUserDeactivation(
			&recordingCommands{}, stubQueries{err: boom}, discardLogger())

		if err := h.Handle(ctx, deactivated(user.NewID())); !errors.Is(err, boom) {
			t.Fatalf("got %v, want the query error", err)
		}
	})
}

func TestTheCascadeWithRealOrderServices(t *testing.T) {
	// The same reaction, wired over real collaborators instead of stubs and
	// below the HTTP layer, so a regression localises here rather than in the
	// end-to-end handler test.
	ctx := context.Background()
	log := discardLogger()

	users := memory.NewUserRepository()
	orders := memory.NewOrderRepository()
	read := memory.NewOrderReadStore()

	// The bus carries order events to the projector, which keeps the read model
	// the handler queries in sync — the real wiring, minus the outbox.
	bus := eventbus.New(log)
	projector := orderapp.NewProjector(orders, read, log)
	for _, name := range projector.EventNames() {
		bus.Subscribe(name, projector.Handle)
	}

	orderSvc := orderapp.NewService(
		orders, integration.NewUserCustomerChecker(users), memory.NewTransactor(), bus, nil, log)
	queries := orderapp.NewQueryService(read)

	customerID := saveUser(t, users, true)

	open1, err := orderSvc.Create(ctx, orderapp.CreateCommand{CustomerID: customerID.String(), Currency: "EUR"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	open2, err := orderSvc.Create(ctx, orderapp.CreateCommand{CustomerID: customerID.String(), Currency: "EUR"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A third order belonging to somebody else must not be touched.
	otherCustomer := uuid.Must(uuid.NewV7()).String()
	foreignSvc := orderapp.NewService(
		orders, alwaysActive{}, memory.NewTransactor(), bus, nil, log)
	foreign, err := foreignSvc.Create(ctx, orderapp.CreateCommand{CustomerID: otherCustomer, Currency: "EUR"})
	if err != nil {
		t.Fatalf("Create for another customer: %v", err)
	}

	h := integration.NewCancelOrdersOnUserDeactivation(orderSvc, queries, log)
	if err := h.Handle(ctx, deactivated(customerID)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	for _, id := range []string{open1.ID, open2.ID} {
		got, err := queries.GetOrder(ctx, id)
		if err != nil {
			t.Fatalf("GetOrder(%s): %v", id, err)
		}
		if got.Status != "cancelled" {
			t.Errorf("order %s: status got %q, want %q", id, got.Status, "cancelled")
		}
	}

	untouched, err := queries.GetOrder(ctx, foreign.ID)
	if err != nil {
		t.Fatalf("GetOrder(foreign): %v", err)
	}
	if untouched.Status != "draft" {
		t.Errorf("another customer's order was affected: status got %q, want %q", untouched.Status, "draft")
	}
}

// alwaysActive lets the fixture create an order for a customer that has no user
// record, without weakening the checker used by the assertions above.
type alwaysActive struct{}

func (alwaysActive) IsActiveCustomer(context.Context, string) (bool, error) { return true, nil }
