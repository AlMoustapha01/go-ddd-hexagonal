package order

import (
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Order domain events. They embed shared.BaseEvent so they ride the same event
// bus as user events. Every state change emits one, which lets the CQRS
// projector keep the read model in sync for the whole lifecycle.

// base is what every order event has in common: when it happened, and which
// order it concerns. Embedding it declares the OrderID field and the AggregateID
// method once instead of five times, and is what makes every order event satisfy
// shared.Keyed so the projector and the outbox can route them without knowing
// the concrete types.
//
// It lives here rather than in the shared kernel on purpose. shared.Keyed is an
// OPTIONAL interface, discovered by type assertion; user events deliberately do
// not implement it. Hoisting AggregateID onto shared.BaseEvent would make every
// event in the system Keyed at once and erase that distinction.
type base struct {
	shared.BaseEvent
	OrderID ID
}

// AggregateID returns the order id (implements shared.Keyed).
func (e base) AggregateID() string { return e.OrderID.String() }

// newBase builds the common part of an order event.
func newBase(id ID, at time.Time) base {
	return base{BaseEvent: shared.EventAt(at), OrderID: id}
}

// Created is emitted when a new draft order is opened.
type Created struct {
	base
	CustomerID CustomerID
}

// EventName identifies the event on the bus.
func (Created) EventName() string { return "order.created" }

// Compile-time proof that embedding base really does carry both interfaces
// through to the concrete events the bus and the projector consume.
var (
	_ shared.Event = Created{}
	_ shared.Keyed = Created{}
)

// LineAdded is emitted when a product line is added to a draft order.
type LineAdded struct {
	base
	ProductID string
	Quantity  int
}

// EventName identifies the event on the bus.
func (LineAdded) EventName() string { return "order.line_added" }

// Placed is emitted when a draft order is submitted.
type Placed struct {
	base
	CustomerID CustomerID
	Total      Money
}

func (Placed) EventName() string { return "order.placed" }

// Paid is emitted when a placed order is paid.
type Paid struct {
	base
}

func (Paid) EventName() string { return "order.paid" }

// Cancelled is emitted when an order is cancelled.
type Cancelled struct {
	base
	Reason string
}

func (Cancelled) EventName() string { return "order.cancelled" }
