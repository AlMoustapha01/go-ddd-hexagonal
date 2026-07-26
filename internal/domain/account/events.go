package account

import (
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Account events. In an EVENT-SOURCED aggregate the events are the source of
// truth — the current state is derived by replaying them. They therefore carry
// plain, serializable data (no value objects with unexported fields), so they
// can be persisted to and read back from the event store faithfully.

// base is what every account event has in common: when it happened, and which
// account it concerns. Embedding it declares the AccountID field and the
// AggregateID method once instead of three times.
//
// It lives here rather than in the shared kernel on purpose. shared.Keyed is an
// OPTIONAL interface, discovered by type assertion; user events deliberately do
// not implement it. Hoisting AggregateID onto shared.BaseEvent would make every
// event in the system Keyed at once — and would add a field the codec below
// would then have to persist and read back for the lifetime of the stream.
type base struct {
	shared.BaseEvent
	AccountID ID
}

// AggregateID implements shared.Keyed.
func (e base) AggregateID() string { return e.AccountID.String() }

// newBase builds the common part of an account event.
func newBase(id ID, at time.Time) base {
	return base{BaseEvent: shared.EventAt(at), AccountID: id}
}

// Opened is the first event of every account's stream.
type Opened struct {
	base
	Currency string
}

// EventName identifies the event.
func (Opened) EventName() string { return "account.opened" }

// Compile-time proof that embedding base really does carry both interfaces
// through to the concrete events the event store and the bus consume.
var (
	_ shared.Event = Opened{}
	_ shared.Keyed = Opened{}
)

// Deposited records money added to the account.
type Deposited struct {
	base
	Amount   int64 // minor units
	Currency string
}

// EventName identifies the event.
func (Deposited) EventName() string { return "account.deposited" }

// Withdrawn records money removed from the account.
type Withdrawn struct {
	base
	Amount   int64 // minor units
	Currency string
}

// EventName identifies the event.
func (Withdrawn) EventName() string { return "account.withdrawn" }
