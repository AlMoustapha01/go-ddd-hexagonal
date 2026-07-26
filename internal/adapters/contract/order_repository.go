package contract

import (
	"context"
	"errors"
	"testing"

	order "github.com/example/go-ddd-hex/internal/domain/order"
)

// OrderRepository runs the contract every order.Repository implementation must
// satisfy. newRepo must return a fresh, empty repository on every call.
func OrderRepository(t *testing.T, newRepo func(*testing.T) order.Repository) {
	ctx := context.Background()

	t.Run("Save then FindByID round-trips the aggregate and its lines", func(t *testing.T) {
		repo := newRepo(t)
		customer := mustCustomerID(t)
		want := newOrder(t, customer, at(0),
			mustLine(t, "sku-1", mustMoney(t, 1050, "EUR"), 2),
			mustLine(t, "sku-2", mustMoney(t, 300, "EUR"), 1),
		)

		if err := repo.Save(ctx, want); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := repo.FindByID(ctx, want.ID())
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}

		if !got.ID().Equals(want.ID()) {
			t.Errorf("id: got %s, want %s", got.ID(), want.ID())
		}
		if got.CustomerID().String() != customer.String() {
			t.Errorf("customer: got %s, want %s", got.CustomerID(), customer)
		}
		if got.Currency() != want.Currency() {
			t.Errorf("currency: got %q, want %q", got.Currency(), want.Currency())
		}
		if got.Status() != want.Status() {
			t.Errorf("status: got %q, want %q", got.Status(), want.Status())
		}
		if !got.CreatedAt().Equal(want.CreatedAt()) {
			t.Errorf("createdAt: got %s, want %s", got.CreatedAt(), want.CreatedAt())
		}

		gotLines, wantLines := got.Lines(), want.Lines()
		if len(gotLines) != len(wantLines) {
			t.Fatalf("lines: got %d, want %d", len(gotLines), len(wantLines))
		}
		for i := range wantLines {
			if gotLines[i].ProductID() != wantLines[i].ProductID() {
				t.Errorf("line %d product: got %q, want %q", i, gotLines[i].ProductID(), wantLines[i].ProductID())
			}
			if gotLines[i].Quantity() != wantLines[i].Quantity() {
				t.Errorf("line %d quantity: got %d, want %d", i, gotLines[i].Quantity(), wantLines[i].Quantity())
			}
			// Money must survive intact — amount AND currency.
			if !gotLines[i].UnitPrice().Equals(wantLines[i].UnitPrice()) {
				t.Errorf("line %d price: got %s, want %s", i, gotLines[i].UnitPrice(), wantLines[i].UnitPrice())
			}
		}

		// The total is derived from the persisted lines, so it doubles as a
		// check that the amounts came back in minor units, not rescaled.
		total, err := got.Total()
		if err != nil {
			t.Fatalf("Total: %v", err)
		}
		if total.Amount() != 2*1050+300 {
			t.Errorf("total: got %d, want %d", total.Amount(), 2*1050+300)
		}
	})

	t.Run("Save updates an existing order in place", func(t *testing.T) {
		repo := newRepo(t)
		customer := mustCustomerID(t)
		o := newOrder(t, customer, at(0), mustLine(t, "sku-1", mustMoney(t, 100, "EUR"), 1))
		if err := repo.Save(ctx, o); err != nil {
			t.Fatalf("first Save: %v", err)
		}

		placed := order.Rehydrate(o.ID(), customer, "EUR", order.StatusPlaced,
			[]order.Line{mustLine(t, "sku-1", mustMoney(t, 100, "EUR"), 5)}, o.CreatedAt(), at(1))
		if err := repo.Save(ctx, placed); err != nil {
			t.Fatalf("second Save: %v", err)
		}

		got, err := repo.FindByID(ctx, o.ID())
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if got.Status() != order.StatusPlaced {
			t.Errorf("status: got %q, want %q", got.Status(), order.StatusPlaced)
		}
		if lines := got.Lines(); len(lines) != 1 || lines[0].Quantity() != 5 {
			t.Errorf("lines were not replaced: %+v", lines)
		}

		all, err := repo.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(all) != 1 {
			t.Errorf("an update must not create a second row: got %d orders, want 1", len(all))
		}
	})

	t.Run("an order with no lines round-trips", func(t *testing.T) {
		repo := newRepo(t)
		o := newOrder(t, mustCustomerID(t), at(0))
		if err := repo.Save(ctx, o); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := repo.FindByID(ctx, o.ID())
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if len(got.Lines()) != 0 {
			t.Errorf("got %d lines, want 0", len(got.Lines()))
		}
	})

	t.Run("FindByID reports ErrNotFound for an unknown id", func(t *testing.T) {
		repo := newRepo(t)
		if _, err := repo.FindByID(ctx, order.NewID()); !errors.Is(err, order.ErrNotFound) {
			t.Fatalf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("ListByCustomer returns only that customer's orders, newest first", func(t *testing.T) {
		repo := newRepo(t)
		mine, other := mustCustomerID(t), mustCustomerID(t)

		older := newOrder(t, mine, at(0))
		newer := newOrder(t, mine, at(1))
		foreign := newOrder(t, other, at(2))
		for _, o := range []*order.Order{older, foreign, newer} {
			if err := repo.Save(ctx, o); err != nil {
				t.Fatalf("Save: %v", err)
			}
		}

		got, err := repo.ListByCustomer(ctx, mine)
		if err != nil {
			t.Fatalf("ListByCustomer: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d orders, want 2", len(got))
		}
		if !got[0].ID().Equals(newer.ID()) {
			t.Errorf("first result: got %s, want the newest order %s", got[0].ID(), newer.ID())
		}
		if !got[1].ID().Equals(older.ID()) {
			t.Errorf("second result: got %s, want the older order %s", got[1].ID(), older.ID())
		}
	})

	t.Run("ListByCustomer returns a non-nil empty slice for an unknown customer", func(t *testing.T) {
		repo := newRepo(t)
		got, err := repo.ListByCustomer(ctx, mustCustomerID(t))
		if err != nil {
			t.Fatalf("ListByCustomer: %v", err)
		}
		if got == nil {
			t.Fatal("got nil, want a non-nil empty slice")
		}
		if len(got) != 0 {
			t.Fatalf("got %d orders, want 0", len(got))
		}
	})

	t.Run("List returns orders newest first and honours limit and offset", func(t *testing.T) {
		repo := newRepo(t)
		customer := mustCustomerID(t)
		first := newOrder(t, customer, at(0))
		second := newOrder(t, customer, at(1))
		third := newOrder(t, customer, at(2))
		for _, o := range []*order.Order{first, second, third} {
			if err := repo.Save(ctx, o); err != nil {
				t.Fatalf("Save: %v", err)
			}
		}

		all, err := repo.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("got %d orders, want 3", len(all))
		}
		if !all[0].ID().Equals(third.ID()) {
			t.Errorf("newest first: got %s, want %s", all[0].ID(), third.ID())
		}

		page, err := repo.List(ctx, 1, 1)
		if err != nil {
			t.Fatalf("paged List: %v", err)
		}
		if len(page) != 1 {
			t.Fatalf("got %d orders, want 1", len(page))
		}
		if !page[0].ID().Equals(second.ID()) {
			t.Errorf("offset 1: got %s, want %s", page[0].ID(), second.ID())
		}
	})

	t.Run("List returns a non-nil empty slice when there is nothing", func(t *testing.T) {
		repo := newRepo(t)
		got, err := repo.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got == nil {
			t.Fatal("got nil, want a non-nil empty slice")
		}
		if len(got) != 0 {
			t.Fatalf("got %d orders, want 0", len(got))
		}
	})

	t.Run("Delete removes the order, and deleting twice reports ErrNotFound", func(t *testing.T) {
		repo := newRepo(t)
		o := newOrder(t, mustCustomerID(t), at(0))
		if err := repo.Save(ctx, o); err != nil {
			t.Fatalf("Save: %v", err)
		}

		if err := repo.Delete(ctx, o.ID()); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := repo.FindByID(ctx, o.ID()); !errors.Is(err, order.ErrNotFound) {
			t.Fatalf("FindByID after Delete: got %v, want ErrNotFound", err)
		}
		if err := repo.Delete(ctx, o.ID()); !errors.Is(err, order.ErrNotFound) {
			t.Fatalf("second Delete: got %v, want ErrNotFound", err)
		}
	})
}
