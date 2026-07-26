// Package account (application) holds the use cases for the event-sourced
// Account aggregate.
package account

import (
	"context"
	"errors"
	"log/slog"

	domain "github.com/example/go-ddd-hex/internal/domain/account"
)

// Service implements the account command use cases. The command flow is the
// canonical event-sourcing loop: load the stream → rebuild the aggregate →
// decide (produce new events) → append with optimistic concurrency.
type Service struct {
	store domain.EventStore
	log   *slog.Logger
}

// NewService wires the command service over an event store.
func NewService(store domain.EventStore, log *slog.Logger) *Service {
	return &Service{store: store, log: log}
}

var _ UseCases = (*Service)(nil)

// Open starts a new account (a new event stream).
func (s *Service) Open(ctx context.Context, cmd OpenCommand) (AccountView, error) {
	a, err := domain.Open(cmd.Currency)
	if err != nil {
		return AccountView{}, err
	}
	// A brand-new stream is expected to be at version 0.
	if err := s.store.Append(ctx, a.ID(), a.Version(), a.PendingEvents()); err != nil {
		return AccountView{}, err
	}
	a.MarkPersisted()
	return toView(a), nil
}

// Deposit adds money to an existing account.
func (s *Service) Deposit(ctx context.Context, cmd AmountCommand) (AccountView, error) {
	return s.execute(ctx, cmd.AccountID, func(a *domain.Account) error { return a.Deposit(cmd.Amount) })
}

// Withdraw removes money, subject to the balance invariant.
func (s *Service) Withdraw(ctx context.Context, cmd AmountCommand) (AccountView, error) {
	return s.execute(ctx, cmd.AccountID, func(a *domain.Account) error { return a.Withdraw(cmd.Amount) })
}

// execute is the load → decide → append loop shared by the mutating commands.
func (s *Service) execute(ctx context.Context, id string, decide func(*domain.Account) error) (AccountView, error) {
	aid, err := domain.IDFromString(id)
	if err != nil {
		return AccountView{}, err
	}
	events, err := s.store.Load(ctx, aid)
	if err != nil {
		return AccountView{}, err
	}

	a := domain.Rehydrate(events)
	expectedVersion := a.Version()

	if err := decide(a); err != nil {
		return AccountView{}, err
	}

	if err := s.store.Append(ctx, aid, expectedVersion, a.PendingEvents()); err != nil {
		// On a concurrency conflict a caller could reload and retry; here we
		// surface it so the transport maps it to 409 Conflict.
		return AccountView{}, err
	}
	a.MarkPersisted()
	return toView(a), nil
}

// IsValidationError reports client-side (400-class) account errors.
func IsValidationError(err error) bool {
	switch {
	case errors.Is(err, domain.ErrInvalidCurrency),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrInsufficientFunds):
		return true
	default:
		return false
	}
}
