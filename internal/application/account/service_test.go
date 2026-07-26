package account_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	app "github.com/example/go-ddd-hex/internal/application/account"
	domain "github.com/example/go-ddd-hex/internal/domain/account"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// stubStore wraps the real in-memory event store — contract-tested against the
// Postgres one — and overrides single methods to reach branches a correct store
// cannot produce on demand.
type stubStore struct {
	domain.EventStore
	appendErr error
	appended  []int // the expectedVersion of each Append call
}

func (s *stubStore) Append(ctx context.Context, id domain.ID, expectedVersion int, events []shared.Event) error {
	s.appended = append(s.appended, expectedVersion)
	if s.appendErr != nil {
		return s.appendErr
	}
	return s.EventStore.Append(ctx, id, expectedVersion, events)
}

func newStore() *stubStore { return &stubStore{EventStore: memory.NewAccountEventStore()} }

func newService(t *testing.T) (*app.Service, *stubStore) {
	t.Helper()
	store := newStore()
	return app.NewService(store, discardLogger()), store
}

func open(t *testing.T, s *app.Service, currency string) app.AccountView {
	t.Helper()
	v, err := s.Open(context.Background(), app.OpenCommand{Currency: currency})
	if err != nil {
		t.Fatalf("Open(%q): %v", currency, err)
	}
	return v
}

func TestOpen(t *testing.T) {
	ctx := context.Background()

	t.Run("starts a stream at version 1", func(t *testing.T) {
		svc, store := newService(t)

		got := open(t, svc, "EUR")
		if got.ID == "" {
			t.Error("an account should have an identity")
		}
		if got.Currency != "EUR" {
			t.Errorf("currency: got %q, want %q", got.Currency, "EUR")
		}
		if got.BalanceMinor != 0 {
			t.Errorf("balance: got %d, want 0", got.BalanceMinor)
		}
		if got.Version != 1 {
			t.Errorf("version: got %d, want 1 — the opening event is persisted", got.Version)
		}
		// A brand-new stream must be appended at expected version 0.
		if len(store.appended) != 1 || store.appended[0] != 0 {
			t.Errorf("expectedVersion passed to the store: got %v, want [0]", store.appended)
		}
	})

	t.Run("rejects a malformed currency", func(t *testing.T) {
		svc, store := newService(t)
		if _, err := svc.Open(ctx, app.OpenCommand{Currency: "EUROS"}); !errors.Is(err, domain.ErrInvalidCurrency) {
			t.Fatalf("got %v, want ErrInvalidCurrency", err)
		}
		if len(store.appended) != 0 {
			t.Error("nothing should have been appended")
		}
	})

	t.Run("propagates a store failure", func(t *testing.T) {
		boom := errors.New("event store is unreachable")
		store := newStore()
		store.appendErr = boom
		svc := app.NewService(store, discardLogger())

		if _, err := svc.Open(ctx, app.OpenCommand{Currency: "EUR"}); !errors.Is(err, boom) {
			t.Fatalf("got %v, want the store error", err)
		}
	})
}

func TestDepositAndWithdrawFoldTheStream(t *testing.T) {
	// The event-sourcing loop: load the stream, rebuild, decide, append. The
	// balance is never stored — every command re-derives it.
	ctx := context.Background()
	svc, store := newService(t)
	acc := open(t, svc, "EUR")

	after, err := svc.Deposit(ctx, app.AmountCommand{AccountID: acc.ID, Amount: 1500})
	if err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if after.BalanceMinor != 1500 {
		t.Errorf("balance: got %d, want 1500", after.BalanceMinor)
	}
	if after.Version != 2 {
		t.Errorf("version: got %d, want 2", after.Version)
	}

	after, err = svc.Withdraw(ctx, app.AmountCommand{AccountID: acc.ID, Amount: 500})
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if after.BalanceMinor != 1000 {
		t.Errorf("balance: got %d, want 1000", after.BalanceMinor)
	}
	if after.Version != 3 {
		t.Errorf("version: got %d, want 3", after.Version)
	}

	// Each command must append at the version it just loaded. Getting this wrong
	// would make the SECOND command conflict against itself — which is exactly
	// why the sequence above is run rather than a single deposit.
	want := []int{0, 1, 2}
	if len(store.appended) != len(want) {
		t.Fatalf("expectedVersion sequence: got %v, want %v", store.appended, want)
	}
	for i := range want {
		if store.appended[i] != want[i] {
			t.Errorf("append %d: expectedVersion got %d, want %d", i, store.appended[i], want[i])
		}
	}
}

func TestBalanceInvariantAndAmountValidation(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	acc := open(t, svc, "EUR")

	if _, err := svc.Withdraw(ctx, app.AmountCommand{AccountID: acc.ID, Amount: 1}); !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Errorf("overdrawing: got %v, want ErrInsufficientFunds", err)
	}
	for _, amount := range []int64{0, -100} {
		if _, err := svc.Deposit(ctx, app.AmountCommand{AccountID: acc.ID, Amount: amount}); !errors.Is(err, domain.ErrInvalidAmount) {
			t.Errorf("depositing %d: got %v, want ErrInvalidAmount", amount, err)
		}
		if _, err := svc.Withdraw(ctx, app.AmountCommand{AccountID: acc.ID, Amount: amount}); !errors.Is(err, domain.ErrInvalidAmount) {
			t.Errorf("withdrawing %d: got %v, want ErrInvalidAmount", amount, err)
		}
	}
}

func TestCommandsOnMissingOrMalformedID(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)

	for name, id := range map[string]string{
		"malformed id": "not-a-uuid",
		"unknown id":   domain.NewID().String(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.Deposit(ctx, app.AmountCommand{AccountID: id, Amount: 100}); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("Deposit: got %v, want ErrNotFound", err)
			}
			if _, err := svc.Withdraw(ctx, app.AmountCommand{AccountID: id, Amount: 100}); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("Withdraw: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestAConcurrencyConflictSurfacesToTheCaller(t *testing.T) {
	// It must NOT be swallowed or retried here: the transport maps it to 409 so
	// the caller can reload and decide again. Silently retrying would apply a
	// decision taken against stale state.
	ctx := context.Background()
	svc, store := newService(t)
	acc := open(t, svc, "EUR")

	store.appendErr = domain.ErrConcurrencyConflict

	if _, err := svc.Deposit(ctx, app.AmountCommand{AccountID: acc.ID, Amount: 100}); !errors.Is(err, domain.ErrConcurrencyConflict) {
		t.Fatalf("got %v, want ErrConcurrencyConflict", err)
	}
}

func TestQueriesReplayTheStream(t *testing.T) {
	ctx := context.Background()
	store := newStore()
	svc := app.NewService(store, discardLogger())
	queries := app.NewQueryService(store)

	acc := open(t, svc, "EUR")
	if _, err := svc.Deposit(ctx, app.AmountCommand{AccountID: acc.ID, Amount: 1500}); err != nil {
		t.Fatalf("Deposit: %v", err)
	}
	if _, err := svc.Withdraw(ctx, app.AmountCommand{AccountID: acc.ID, Amount: 400}); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	t.Run("Get folds the stream into the current state", func(t *testing.T) {
		got, err := queries.Get(ctx, acc.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.BalanceMinor != 1100 {
			t.Errorf("balance: got %d, want 1100", got.BalanceMinor)
		}
		if got.Version != 3 {
			t.Errorf("version: got %d, want 3", got.Version)
		}
	})

	t.Run("History is the audit trail, in order", func(t *testing.T) {
		got, err := queries.History(ctx, acc.ID)
		if err != nil {
			t.Fatalf("History: %v", err)
		}
		want := []struct {
			event  string
			amount int64
		}{
			{"account.opened", 0},
			{"account.deposited", 1500},
			{"account.withdrawn", 400},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d entries, want %d", len(got), len(want))
		}
		for i, w := range want {
			if got[i].Sequence != i+1 {
				t.Errorf("entry %d: sequence got %d, want %d", i, got[i].Sequence, i+1)
			}
			if got[i].Event != w.event {
				t.Errorf("entry %d: event got %q, want %q", i, got[i].Event, w.event)
			}
			if got[i].AmountMinor != w.amount {
				t.Errorf("entry %d: amount got %d, want %d", i, got[i].AmountMinor, w.amount)
			}
		}
	})

	t.Run("both report ErrNotFound for an unknown or malformed id", func(t *testing.T) {
		for _, id := range []string{"not-a-uuid", domain.NewID().String()} {
			if _, err := queries.Get(ctx, id); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("Get(%q): got %v, want ErrNotFound", id, err)
			}
			if _, err := queries.History(ctx, id); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("History(%q): got %v, want ErrNotFound", id, err)
			}
		}
	})
}

func TestIsValidationError(t *testing.T) {
	for _, err := range []error{domain.ErrInvalidCurrency, domain.ErrInvalidAmount, domain.ErrInsufficientFunds} {
		if !app.IsValidationError(err) {
			t.Errorf("%v should be classed as a validation error", err)
		}
	}
	// A concurrency conflict is NOT a client mistake: it maps to 409, not 400.
	for _, err := range []error{domain.ErrNotFound, domain.ErrConcurrencyConflict, errors.New("boom"), nil} {
		if app.IsValidationError(err) {
			t.Errorf("%v should NOT be classed as a validation error", err)
		}
	}
}
