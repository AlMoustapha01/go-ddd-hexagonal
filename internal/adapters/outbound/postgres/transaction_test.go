package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	pg "github.com/example/go-ddd-hex/internal/adapters/outbound/postgres"
	order "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// The Unit of Work has NO shared contract with the in-memory driver, and that is
// deliberate: memory.Transactor is a documented no-op ("There is no real
// transaction, so writes are NOT atomic together"). Only Postgres can make the
// all-or-nothing promise the Outbox pattern rests on, so only Postgres is held
// to it — here.
//
// What these tests actually prove is the correctness of the outbox: the
// aggregate row and its event row live or die together, which is what removes
// the dual-write hazard.
//
// NOTE for future work: enrolment comes from queryEngine reading the
// transaction off the context, and only OrderRepository.Save and OutboxStore.Add
// use it. OrderRepository.Delete and the whole UserRepository go straight to the
// pool, so a write made through them inside a Within would silently escape the
// transaction. That is harmless today (no use case does it) but it is a trap;
// if either is ever called inside a Within, route it through queryEngine and add
// a case below.

func TestUnitOfWorkCommitsAggregateAndOutboxTogether(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()

	uow := pg.NewTransactor(pool)
	orders := pg.NewOrderRepository(pool)
	outbox := pg.NewOutboxStore(pool)

	o := draftOrder(t)
	rec := record(o.ID().String())

	err := uow.Within(ctx, func(ctx context.Context) error {
		if err := orders.Save(ctx, o); err != nil {
			return err
		}
		return outbox.Add(ctx, rec)
	})
	if err != nil {
		t.Fatalf("Within: %v", err)
	}

	if _, err := orders.FindByID(ctx, o.ID()); err != nil {
		t.Errorf("the order should have been committed: %v", err)
	}
	pending, err := outbox.FetchUnpublished(ctx, 10)
	if err != nil {
		t.Fatalf("FetchUnpublished: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d outbox records, want 1", len(pending))
	}
	if pending[0].ID != rec.ID {
		t.Errorf("got record %q, want %q", pending[0].ID, rec.ID)
	}
}

func TestUnitOfWorkRollsBackAggregateAndOutboxTogether(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()

	uow := pg.NewTransactor(pool)
	orders := pg.NewOrderRepository(pool)
	outbox := pg.NewOutboxStore(pool)

	o := draftOrder(t)
	boom := errors.New("the use case failed after writing")

	err := uow.Within(ctx, func(ctx context.Context) error {
		if err := orders.Save(ctx, o); err != nil {
			return err
		}
		if err := outbox.Add(ctx, record(o.ID().String())); err != nil {
			return err
		}
		return boom // both writes must be undone
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Within: got %v, want the caller's error", err)
	}

	// Neither side may survive: an event without its state change is exactly
	// the dual-write hazard the outbox exists to prevent, and so is the reverse.
	if _, err := orders.FindByID(ctx, o.ID()); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("the order should have been rolled back: got %v, want ErrNotFound", err)
	}
	pending, err := outbox.FetchUnpublished(ctx, 10)
	if err != nil {
		t.Fatalf("FetchUnpublished: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("got %d outbox records, want 0 — the event must not outlive the rolled-back state", len(pending))
	}
}

func TestUnitOfWorkSeesItsOwnUncommittedWrites(t *testing.T) {
	// Proves enrolment rather than coincidence: if Save had gone to the pool
	// instead of the transaction, the row would already be visible outside it.
	pool := freshDB(t)
	ctx := context.Background()

	uow := pg.NewTransactor(pool)
	orders := pg.NewOrderRepository(pool)
	o := draftOrder(t)

	err := uow.Within(ctx, func(txCtx context.Context) error {
		if err := orders.Save(txCtx, o); err != nil {
			return err
		}
		// A read on a plain context uses the pool, i.e. another connection,
		// which must not see the uncommitted row.
		if _, err := orders.FindByID(ctx, o.ID()); !errors.Is(err, order.ErrNotFound) {
			t.Errorf("outside the transaction: got %v, want ErrNotFound", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Within: %v", err)
	}

	if _, err := orders.FindByID(ctx, o.ID()); err != nil {
		t.Errorf("after commit the order should be visible: %v", err)
	}
}

// draftOrder builds a persisted-shaped draft order fixture.
func draftOrder(t *testing.T) *order.Order {
	t.Helper()

	customerID, err := order.CustomerIDFromString(uuid.Must(uuid.NewV7()).String())
	if err != nil {
		t.Fatalf("CustomerIDFromString: %v", err)
	}
	price, err := order.NewMoney(1050, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	line, err := order.NewLine("sku-1", price, 2)
	if err != nil {
		t.Fatalf("NewLine: %v", err)
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return order.Rehydrate(order.NewID(), customerID, "EUR", order.StatusDraft, []order.Line{line}, now, now)
}

// record builds an outbox record for the given aggregate.
func record(aggregateID string) shared.OutboxRecord {
	return shared.OutboxRecord{
		ID:          uuid.Must(uuid.NewV7()).String(),
		EventName:   "order.created",
		AggregateID: aggregateID,
		Payload:     []byte(`{"event":"order.created"}`),
		OccurredAt:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}
