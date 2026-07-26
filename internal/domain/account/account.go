// Package account is an EVENT-SOURCED aggregate (a simple wallet), the
// counterpart to the state-stored Order aggregate.
//
// Nothing here stores "the balance". The truth is the ordered stream of events
// (Opened, Deposited, Withdrawn). The current state is a left-fold over that
// stream: apply each event in turn. A command validates against the folded
// state and, if allowed, appends NEW events — it never mutates a stored row.
package account

import (
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Account is rebuilt from its event stream. `version` is the number of events
// already persisted (used for optimistic concurrency); `pending` holds new,
// uncommitted events produced by the current command.
type Account struct {
	id       ID
	currency string
	balance  int64 // minor units — DERIVED from events, never set directly
	opened   bool

	version int
	pending []shared.Event
}

// Open is the factory: it starts a new stream with an Opened event.
func Open(currency string) (*Account, error) {
	if len(currency) != 3 {
		return nil, ErrInvalidCurrency
	}
	a := &Account{}
	a.raise(Opened{BaseEvent: newBase(time.Now().UTC()), AccountID: NewID(), Currency: currency})
	return a, nil
}

// Rehydrate reconstructs an account by replaying its persisted events. This is
// the heart of event sourcing: state = fold(events).
func Rehydrate(events []shared.Event) *Account {
	a := &Account{}
	for _, e := range events {
		a.mutate(e)
		a.version++
	}
	return a
}

// Deposit appends a Deposited event after validating the amount.
func (a *Account) Deposit(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	a.raise(Deposited{BaseEvent: newBase(time.Now().UTC()), AccountID: a.id, Amount: amount, Currency: a.currency})
	return nil
}

// Withdraw appends a Withdrawn event, enforcing the balance invariant against
// the CURRENT folded state.
func (a *Account) Withdraw(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	if amount > a.balance {
		return ErrInsufficientFunds
	}
	a.raise(Withdrawn{BaseEvent: newBase(time.Now().UTC()), AccountID: a.id, Amount: amount, Currency: a.currency})
	return nil
}

// raise applies an event to the state AND records it as pending (to be
// persisted). mutate alone is used when replaying already-persisted events.
func (a *Account) raise(e shared.Event) {
	a.mutate(e)
	a.pending = append(a.pending, e)
}

// mutate is the state transition function: given an event, evolve the state.
// It is the single place that knows how each event changes the account, and it
// is used both for new events and for replay — guaranteeing they agree.
func (a *Account) mutate(e shared.Event) {
	switch ev := e.(type) {
	case Opened:
		a.id = ev.AccountID
		a.currency = ev.Currency
		a.balance = 0
		a.opened = true
	case Deposited:
		a.balance += ev.Amount
	case Withdrawn:
		a.balance -= ev.Amount
	}
}

// Getters — read-only.
func (a *Account) ID() ID           { return a.id }
func (a *Account) Currency() string { return a.currency }
func (a *Account) Balance() int64   { return a.balance }
func (a *Account) IsOpen() bool     { return a.opened }

// Version returns the number of PERSISTED events (excludes pending). The command
// side passes it as the expected version when appending, for optimistic
// concurrency control.
func (a *Account) Version() int { return a.version }

// PendingEvents returns the new uncommitted events (without clearing them), so
// the command side can persist them and then call MarkPersisted.
func (a *Account) PendingEvents() []shared.Event { return a.pending }

// MarkPersisted advances the version by the number of pending events and clears
// them — call it after the event store has durably appended them.
func (a *Account) MarkPersisted() {
	a.version += len(a.pending)
	a.pending = nil
}
