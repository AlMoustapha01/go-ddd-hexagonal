package memory_test

import (
	"context"
	"testing"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	account "github.com/example/go-ddd-hex/internal/domain/account"
)

func TestAccountEventStoreOptimisticConcurrency(t *testing.T) {
	ctx := context.Background()
	store := memory.NewAccountEventStore()

	a, _ := account.Open("EUR")
	id := a.ID()

	// Append the opening event at version 0.
	if err := store.Append(ctx, id, 0, a.PendingEvents()); err != nil {
		t.Fatalf("initial append: %v", err)
	}

	// Two concurrent writers both loaded version 1 and try to append at it.
	events, _ := store.Load(ctx, id)
	loaded := account.Rehydrate(events)
	version := loaded.Version()

	_ = loaded.Deposit(100)
	if err := store.Append(ctx, id, version, loaded.PendingEvents()); err != nil {
		t.Fatalf("first writer append: %v", err)
	}

	// Second writer with the stale version must be rejected.
	other := account.Rehydrate(events)
	_ = other.Deposit(999)
	if err := store.Append(ctx, id, version, other.PendingEvents()); err != account.ErrConcurrencyConflict {
		t.Fatalf("stale append: got %v, want ErrConcurrencyConflict", err)
	}
}

func TestAccountEventStoreLoadUnknown(t *testing.T) {
	store := memory.NewAccountEventStore()
	if _, err := store.Load(context.Background(), account.NewID()); err != account.ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
