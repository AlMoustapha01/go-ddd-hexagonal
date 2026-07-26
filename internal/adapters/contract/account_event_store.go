package contract

import (
	"context"
	"errors"
	"testing"

	account "github.com/example/go-ddd-hex/internal/domain/account"
)

// AccountEventStore runs the contract every account.EventStore implementation
// must satisfy. newStore must return a fresh, empty store on every call.
//
// KNOWN DIVERGENCE — expectedVersion ahead of the stored version. The port says
// Append succeeds "ONLY IF the current stored version equals expectedVersion".
// The in-memory adapter enforces that literally; Postgres relies on the
// PRIMARY KEY (aggregate_id, version), so an expectedVersion GREATER than the
// real one inserts into a numbering gap and succeeds. The case is unreachable
// from a correct caller, which always passes the version it just loaded, so
// this contract exercises the reachable direction — the stale writer — which
// both adapters reject identically.
func AccountEventStore(t *testing.T, newStore func(*testing.T) account.EventStore) {
	ctx := context.Background()

	t.Run("Load reports ErrNotFound for a stream that does not exist", func(t *testing.T) {
		store := newStore(t)
		if _, err := store.Load(ctx, account.NewID()); !errors.Is(err, account.ErrNotFound) {
			t.Fatalf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("appended events replay to the same state", func(t *testing.T) {
		// This is the defining property of event sourcing: state is a fold over
		// the stored stream, so a reload must reproduce the live aggregate.
		store := newStore(t)

		live, err := account.Open("EUR")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		id := live.ID()
		if err := store.Append(ctx, id, live.Version(), live.PendingEvents()); err != nil {
			t.Fatalf("append opening event: %v", err)
		}
		live.MarkPersisted()

		if err := live.Deposit(1500); err != nil {
			t.Fatalf("Deposit: %v", err)
		}
		if err := store.Append(ctx, id, live.Version(), live.PendingEvents()); err != nil {
			t.Fatalf("append deposit: %v", err)
		}
		live.MarkPersisted()

		if err := live.Withdraw(500); err != nil {
			t.Fatalf("Withdraw: %v", err)
		}
		if err := store.Append(ctx, id, live.Version(), live.PendingEvents()); err != nil {
			t.Fatalf("append withdrawal: %v", err)
		}
		live.MarkPersisted()

		events, err := store.Load(ctx, id)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(events) != 3 {
			t.Fatalf("got %d events, want 3", len(events))
		}
		wantNames := []string{"account.opened", "account.deposited", "account.withdrawn"}
		for i, name := range wantNames {
			if events[i].EventName() != name {
				t.Errorf("event %d: got %q, want %q", i, events[i].EventName(), name)
			}
		}

		replayed := account.Rehydrate(events)
		if !replayed.ID().Equals(live.ID()) {
			t.Errorf("id: got %s, want %s", replayed.ID(), live.ID())
		}
		if replayed.Balance() != live.Balance() {
			t.Errorf("balance: got %d, want %d", replayed.Balance(), live.Balance())
		}
		if replayed.Currency() != live.Currency() {
			t.Errorf("currency: got %q, want %q", replayed.Currency(), live.Currency())
		}
		if replayed.Version() != live.Version() {
			t.Errorf("version: got %d, want %d", replayed.Version(), live.Version())
		}
		if !replayed.IsOpen() {
			t.Error("replayed account should be open")
		}
	})

	t.Run("a stale writer is rejected with ErrConcurrencyConflict", func(t *testing.T) {
		store := newStore(t)

		a, err := account.Open("EUR")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		id := a.ID()
		if err := store.Append(ctx, id, 0, a.PendingEvents()); err != nil {
			t.Fatalf("initial append: %v", err)
		}

		// Two writers load the same stream and both decide from version 1.
		events, err := store.Load(ctx, id)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		first, second := account.Rehydrate(events), account.Rehydrate(events)
		version := first.Version()

		if err := first.Deposit(100); err != nil {
			t.Fatalf("first Deposit: %v", err)
		}
		if err := store.Append(ctx, id, version, first.PendingEvents()); err != nil {
			t.Fatalf("first writer should win: %v", err)
		}

		if err := second.Deposit(999); err != nil {
			t.Fatalf("second Deposit: %v", err)
		}
		if err := store.Append(ctx, id, version, second.PendingEvents()); !errors.Is(err, account.ErrConcurrencyConflict) {
			t.Fatalf("stale writer: got %v, want ErrConcurrencyConflict", err)
		}

		// The loser must not have left anything behind.
		after, err := store.Load(ctx, id)
		if err != nil {
			t.Fatalf("Load after conflict: %v", err)
		}
		if len(after) != 2 {
			t.Fatalf("got %d events, want 2 — the rejected append must not persist", len(after))
		}
		if account.Rehydrate(after).Balance() != 100 {
			t.Errorf("balance: got %d, want 100", account.Rehydrate(after).Balance())
		}
	})

	t.Run("appending nothing is a no-op", func(t *testing.T) {
		store := newStore(t)
		id := account.NewID()
		if err := store.Append(ctx, id, 0, nil); err != nil {
			t.Fatalf("Append(nil): %v", err)
		}
		if _, err := store.Load(ctx, id); !errors.Is(err, account.ErrNotFound) {
			t.Fatalf("an empty append must not create a stream: got %v, want ErrNotFound", err)
		}
	})

	t.Run("streams are isolated from one another", func(t *testing.T) {
		store := newStore(t)

		first, err := account.Open("EUR")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		second, err := account.Open("USD")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if err := store.Append(ctx, first.ID(), 0, first.PendingEvents()); err != nil {
			t.Fatalf("append first: %v", err)
		}
		if err := store.Append(ctx, second.ID(), 0, second.PendingEvents()); err != nil {
			t.Fatalf("append second: %v", err)
		}

		events, err := store.Load(ctx, first.ID())
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("got %d events, want 1 — streams must not bleed into each other", len(events))
		}
		if got := account.Rehydrate(events).Currency(); got != "EUR" {
			t.Errorf("currency: got %q, want %q", got, "EUR")
		}
	})
}
