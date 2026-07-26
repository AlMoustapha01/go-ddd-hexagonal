package account

import (
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Account events. In an EVENT-SOURCED aggregate the events are the source of
// truth — the current state is derived by replaying them. They therefore carry
// plain, serializable data (no value objects with unexported fields), so they
// can be persisted to and read back from the event store faithfully.

// Opened is the first event of every account's stream.
type Opened struct {
	shared.BaseEvent
	AccountID ID
	Currency  string
}

// EventName identifies the event.
func (Opened) EventName() string { return "account.opened" }

// AggregateID implements shared.Keyed.
func (e Opened) AggregateID() string { return e.AccountID.String() }

// Deposited records money added to the account.
type Deposited struct {
	shared.BaseEvent
	AccountID ID
	Amount    int64 // minor units
	Currency  string
}

// EventName identifies the event.
func (Deposited) EventName() string { return "account.deposited" }

// AggregateID implements shared.Keyed.
func (e Deposited) AggregateID() string { return e.AccountID.String() }

// Withdrawn records money removed from the account.
type Withdrawn struct {
	shared.BaseEvent
	AccountID ID
	Amount    int64 // minor units
	Currency  string
}

// EventName identifies the event.
func (Withdrawn) EventName() string { return "account.withdrawn" }

// AggregateID implements shared.Keyed.
func (e Withdrawn) AggregateID() string { return e.AccountID.String() }

func newBase(at time.Time) shared.BaseEvent { return shared.BaseEvent{At: at} }
