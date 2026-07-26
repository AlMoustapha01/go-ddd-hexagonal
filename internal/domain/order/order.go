// Package order is the domain layer for the Order aggregate. Like the user
// package it is pure: no framework, no database, no transport. It references the
// customer only by identity (CustomerID) and never imports the user package.
package order

import (
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Order is an aggregate root. Its lines are part of the aggregate and can only
// be modified through the root, which is the single guardian of the invariants:
//   - lines can be added only while the order is a draft;
//   - every line must share the order's currency;
//   - an order cannot be placed empty;
//   - payment requires a placed order;
//   - a paid or cancelled order is final.
type Order struct {
	id         ID
	customerID CustomerID
	currency   string
	status     Status
	lines      []Line
	createdAt  time.Time
	updatedAt  time.Time

	events []shared.Event
}

// NewDraft creates a new empty draft order for a customer, in a given currency.
func NewDraft(customerID CustomerID, currency string) (*Order, error) {
	if customerID.IsZero() {
		return nil, ErrInvalidCustomer
	}
	if len(currency) != 3 {
		return nil, ErrInvalidCurrency
	}
	now := time.Now().UTC()
	o := &Order{
		id:         NewID(),
		customerID: customerID,
		currency:   currency,
		status:     StatusDraft,
		createdAt:  now,
		updatedAt:  now,
	}
	o.record(Created{base: newBase(o.id, now), CustomerID: customerID})
	return o, nil
}

// Rehydrate reconstructs a persisted order without running factory logic or
// emitting events. Only persistence adapters call it.
func Rehydrate(id ID, customerID CustomerID, currency string, status Status, lines []Line, createdAt, updatedAt time.Time) *Order {
	return &Order{
		id:         id,
		customerID: customerID,
		currency:   currency,
		status:     status,
		lines:      lines,
		createdAt:  createdAt,
		updatedAt:  updatedAt,
	}
}

// AddLine appends a product line. Enforces the draft-only and currency
// invariants.
func (o *Order) AddLine(productID string, unitPrice Money, quantity int) error {
	if o.status != StatusDraft {
		return ErrNotDraft
	}
	if unitPrice.Currency() != o.currency {
		return ErrLineCurrency
	}
	line, err := NewLine(productID, unitPrice, quantity)
	if err != nil {
		return err
	}
	o.lines = append(o.lines, line)
	o.touch()
	o.record(LineAdded{base: newBase(o.id, o.updatedAt), ProductID: line.productID, Quantity: line.quantity})
	return nil
}

// Place transitions draft -> placed, refusing an empty order, and records the
// event carrying the computed total.
func (o *Order) Place() error {
	if o.status != StatusDraft {
		return ErrNotDraft
	}
	if len(o.lines) == 0 {
		return ErrNoLines
	}
	total, err := o.Total()
	if err != nil {
		return err
	}
	o.status = StatusPlaced
	o.touch()
	o.record(Placed{base: newBase(o.id, o.updatedAt), CustomerID: o.customerID, Total: total})
	return nil
}

// Pay transitions placed -> paid.
func (o *Order) Pay() error {
	if o.status != StatusPlaced {
		return ErrNotPlaced
	}
	o.status = StatusPaid
	o.touch()
	o.record(Paid{base: newBase(o.id, o.updatedAt)})
	return nil
}

// Cancel moves the order to cancelled unless it is already final. The reason is
// carried on the event (useful when the cancellation is triggered by another
// aggregate, e.g. the customer being deactivated).
func (o *Order) Cancel(reason string) error {
	if o.status.isFinal() {
		return ErrAlreadyFinalized
	}
	o.status = StatusCancelled
	o.touch()
	o.record(Cancelled{base: newBase(o.id, o.updatedAt), Reason: reason})
	return nil
}

// Total sums the line subtotals in the order currency.
func (o *Order) Total() (Money, error) {
	total := Zero(o.currency)
	for _, l := range o.lines {
		var err error
		total, err = total.Add(l.Subtotal())
		if err != nil {
			return Money{}, err
		}
	}
	return total, nil
}

// Getters — read-only. Lines are returned as a copy so callers can't mutate the
// aggregate's internal slice.
func (o *Order) ID() ID                 { return o.id }
func (o *Order) CustomerID() CustomerID { return o.customerID }
func (o *Order) Currency() string       { return o.currency }
func (o *Order) Status() Status         { return o.status }
func (o *Order) CreatedAt() time.Time   { return o.createdAt }
func (o *Order) UpdatedAt() time.Time   { return o.updatedAt }

func (o *Order) Lines() []Line {
	out := make([]Line, len(o.lines))
	copy(out, o.lines)
	return out
}

// PullEvents returns and clears recorded events.
func (o *Order) PullEvents() []shared.Event {
	events := o.events
	o.events = nil
	return events
}

func (o *Order) record(e shared.Event) { o.events = append(o.events, e) }
func (o *Order) touch()                { o.updatedAt = time.Now().UTC() }
