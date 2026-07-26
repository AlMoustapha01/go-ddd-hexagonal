package order

import (
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Order domain events. They embed shared.BaseEvent so they ride the same event
// bus as user events. Every state change emits one, which lets the CQRS
// projector keep the read model in sync for the whole lifecycle.

// Created is emitted when a new draft order is opened.
type Created struct {
	shared.BaseEvent
	OrderID    ID
	CustomerID CustomerID
}

// EventName identifies the event on the bus.
func (Created) EventName() string { return "order.created" }

// AggregateID returns the order id (implements shared.Keyed).
func (e Created) AggregateID() string { return e.OrderID.String() }

// LineAdded is emitted when a product line is added to a draft order.
type LineAdded struct {
	shared.BaseEvent
	OrderID   ID
	ProductID string
	Quantity  int
}

// EventName identifies the event on the bus.
func (LineAdded) EventName() string { return "order.line_added" }

// AggregateID returns the order id (implements shared.Keyed).
func (e LineAdded) AggregateID() string { return e.OrderID.String() }

// Placed is emitted when a draft order is submitted.
type Placed struct {
	shared.BaseEvent
	OrderID    ID
	CustomerID CustomerID
	Total      Money
}

func (Placed) EventName() string { return "order.placed" }

// AggregateID returns the order id (implements shared.Keyed).
func (e Placed) AggregateID() string { return e.OrderID.String() }

// Paid is emitted when a placed order is paid.
type Paid struct {
	shared.BaseEvent
	OrderID ID
}

func (Paid) EventName() string { return "order.paid" }

// AggregateID returns the order id (implements shared.Keyed).
func (e Paid) AggregateID() string { return e.OrderID.String() }

// Cancelled is emitted when an order is cancelled.
type Cancelled struct {
	shared.BaseEvent
	OrderID ID
	Reason  string
}

func (Cancelled) EventName() string { return "order.cancelled" }

// AggregateID returns the order id (implements shared.Keyed).
func (e Cancelled) AggregateID() string { return e.OrderID.String() }

func newBase(at time.Time) shared.BaseEvent { return shared.BaseEvent{At: at} }
