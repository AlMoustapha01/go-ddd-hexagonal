package contract

import (
	"context"
	"errors"
	"testing"
	"time"

	apporder "github.com/example/go-ddd-hex/internal/application/order"
	domainorder "github.com/example/go-ddd-hex/internal/domain/order"
)

// OrderReadStore runs the contract every apporder.ReadStore implementation must
// satisfy. newStore must return a fresh, empty store on every call.
//
// This is the CQRS read side: a store deliberately separate from the write
// repository, fed by the projector. Two behaviours are worth stating out loud
// because they differ from the write repositories:
//
//   - Delete is a no-op for an unknown id (it does NOT report ErrNotFound),
//     because the projector may legitimately drop a projection that was never
//     built.
//   - The ports take plain strings, but ids are order and customer ids, i.e.
//     UUIDs. A key that is not a UUID cannot match anything and must behave as
//     "no such row" — never as an error. HTTP handlers pass path parameters
//     through unvalidated, so this is the difference between a 404 and a 500.
func OrderReadStore(t *testing.T, newStore func(*testing.T) apporder.ReadStore) {
	ctx := context.Background()

	t.Run("Upsert then Get round-trips the projection and its lines", func(t *testing.T) {
		store := newStore(t)
		want := detailView(newOrderID(), newCustomerID(), "draft", 2400, at(0))

		if err := store.Upsert(ctx, want); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		got, err := store.Get(ctx, want.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if got.ID != want.ID {
			t.Errorf("id: got %q, want %q", got.ID, want.ID)
		}
		if got.CustomerID != want.CustomerID {
			t.Errorf("customer: got %q, want %q", got.CustomerID, want.CustomerID)
		}
		if got.Status != want.Status {
			t.Errorf("status: got %q, want %q", got.Status, want.Status)
		}
		if got.Currency != want.Currency {
			t.Errorf("currency: got %q, want %q", got.Currency, want.Currency)
		}
		if got.TotalMinor != want.TotalMinor {
			t.Errorf("total: got %d, want %d", got.TotalMinor, want.TotalMinor)
		}
		if got.ItemCount != want.ItemCount {
			t.Errorf("itemCount: got %d, want %d", got.ItemCount, want.ItemCount)
		}
		// Equal, not ==: Postgres hands timestamps back in its own location.
		if !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Errorf("updatedAt: got %s, want %s", got.UpdatedAt, want.UpdatedAt)
		}
		if !got.CreatedAt.Equal(want.CreatedAt) {
			t.Errorf("createdAt: got %s, want %s", got.CreatedAt, want.CreatedAt)
		}

		if len(got.Lines) != len(want.Lines) {
			t.Fatalf("lines: got %d, want %d", len(got.Lines), len(want.Lines))
		}
		if got.Lines[0] != want.Lines[0] {
			t.Errorf("line 0: got %+v, want %+v", got.Lines[0], want.Lines[0])
		}
	})

	t.Run("Upsert replaces an existing projection", func(t *testing.T) {
		store := newStore(t)
		first := detailView(newOrderID(), newCustomerID(), "draft", 2400, at(0))
		if err := store.Upsert(ctx, first); err != nil {
			t.Fatalf("first Upsert: %v", err)
		}

		second := first
		second.Status = "placed"
		second.TotalMinor = 9900
		second.UpdatedAt = at(1)
		if err := store.Upsert(ctx, second); err != nil {
			t.Fatalf("second Upsert: %v", err)
		}

		got, err := store.Get(ctx, first.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Status != "placed" {
			t.Errorf("status: got %q, want %q", got.Status, "placed")
		}
		if got.TotalMinor != 9900 {
			t.Errorf("total: got %d, want %d", got.TotalMinor, 9900)
		}

		all, err := store.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(all) != 1 {
			t.Errorf("an upsert must not create a second row: got %d, want 1", len(all))
		}
	})

	t.Run("Get reports ErrNotFound for an unknown id", func(t *testing.T) {
		store := newStore(t)
		if _, err := store.Get(ctx, newOrderID()); !errors.Is(err, domainorder.ErrNotFound) {
			t.Fatalf("got %v, want order.ErrNotFound", err)
		}
	})

	t.Run("a malformed id behaves as no such row, never as an error", func(t *testing.T) {
		// Reachable from the public API: GET /api/v1/orders/{id} passes the raw
		// path parameter through. An adapter that errors here turns a 404 into
		// a 500.
		store := newStore(t)

		if _, err := store.Get(ctx, "not-a-uuid"); !errors.Is(err, domainorder.ErrNotFound) {
			t.Errorf("Get: got %v, want order.ErrNotFound", err)
		}

		got, err := store.ListByCustomer(ctx, "not-a-uuid")
		if err != nil {
			t.Errorf("ListByCustomer: got %v, want nil", err)
		}
		if len(got) != 0 {
			t.Errorf("ListByCustomer: got %d summaries, want 0", len(got))
		}

		if err := store.Delete(ctx, "not-a-uuid"); err != nil {
			t.Errorf("Delete: got %v, want nil", err)
		}
	})

	t.Run("List returns summaries newest first and honours limit and offset", func(t *testing.T) {
		store := newStore(t)
		customer := newCustomerID()
		ids := []string{newOrderID(), newOrderID(), newOrderID()}
		for i, id := range ids {
			if err := store.Upsert(ctx, detailView(id, customer, "draft", 100, at(i))); err != nil {
				t.Fatalf("Upsert: %v", err)
			}
		}

		all, err := store.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("got %d summaries, want 3", len(all))
		}
		if all[0].ID != ids[2] {
			t.Errorf("newest first: got %q, want %q", all[0].ID, ids[2])
		}

		page, err := store.List(ctx, 1, 1)
		if err != nil {
			t.Fatalf("paged List: %v", err)
		}
		if len(page) != 1 {
			t.Fatalf("got %d summaries, want 1", len(page))
		}
		if page[0].ID != ids[1] {
			t.Errorf("offset 1: got %q, want %q", page[0].ID, ids[1])
		}
	})

	t.Run("ListByCustomer returns only that customer's summaries, newest first", func(t *testing.T) {
		store := newStore(t)
		mine, other := newCustomerID(), newCustomerID()
		older, newer, foreign := newOrderID(), newOrderID(), newOrderID()

		if err := store.Upsert(ctx, detailView(older, mine, "draft", 100, at(0))); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if err := store.Upsert(ctx, detailView(newer, mine, "draft", 100, at(1))); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if err := store.Upsert(ctx, detailView(foreign, other, "draft", 100, at(2))); err != nil {
			t.Fatalf("Upsert: %v", err)
		}

		got, err := store.ListByCustomer(ctx, mine)
		if err != nil {
			t.Fatalf("ListByCustomer: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d summaries, want 2", len(got))
		}
		if got[0].ID != newer {
			t.Errorf("newest first: got %q, want %q", got[0].ID, newer)
		}
	})

	t.Run("empty results are non-nil empty slices", func(t *testing.T) {
		// A list method that finds nothing must return [], never nil: the same
		// API would otherwise serialise "null" on one adapter and "[]" on the
		// other.
		store := newStore(t)

		list, err := store.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if list == nil {
			t.Error("List: got nil, want a non-nil empty slice")
		}

		byCustomer, err := store.ListByCustomer(ctx, newCustomerID())
		if err != nil {
			t.Fatalf("ListByCustomer: %v", err)
		}
		if byCustomer == nil {
			t.Error("ListByCustomer: got nil, want a non-nil empty slice")
		}
	})

	t.Run("Delete removes the projection and tolerates an unknown id", func(t *testing.T) {
		store := newStore(t)
		d := detailView(newOrderID(), newCustomerID(), "draft", 100, at(0))
		if err := store.Upsert(ctx, d); err != nil {
			t.Fatalf("Upsert: %v", err)
		}

		if err := store.Delete(ctx, d.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := store.Get(ctx, d.ID); !errors.Is(err, domainorder.ErrNotFound) {
			t.Fatalf("Get after Delete: got %v, want ErrNotFound", err)
		}
		// The projector may drop a projection that was never built; that is not
		// an error.
		if err := store.Delete(ctx, newOrderID()); err != nil {
			t.Fatalf("Delete of an unknown id: got %v, want nil", err)
		}
	})
}

// detailView builds a read-side projection fixture.
func detailView(id, customerID, status string, total int64, updatedAt time.Time) apporder.DetailView {
	return apporder.DetailView{
		SummaryView: apporder.SummaryView{
			ID:         id,
			CustomerID: customerID,
			Status:     status,
			Currency:   "EUR",
			TotalMinor: total,
			ItemCount:  1,
			UpdatedAt:  updatedAt,
		},
		Lines: []apporder.LineView{{
			ProductID: "sku-1",
			UnitPrice: total,
			Currency:  "EUR",
			Quantity:  1,
			Subtotal:  total,
		}},
		CreatedAt: baseTime,
	}
}
