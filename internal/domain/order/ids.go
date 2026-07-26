package order

import "github.com/google/uuid"

// ID is the identity of an Order aggregate.
type ID struct{ value uuid.UUID }

func NewID() ID { return ID{value: uuid.New()} }

func IDFromString(s string) (ID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return ID{}, ErrNotFound
	}
	return ID{value: u}, nil
}

func (id ID) String() string   { return id.value.String() }
func (id ID) Equals(o ID) bool { return id.value == o.value }
func (id ID) IsZero() bool     { return id.value == uuid.Nil }

// CustomerID references the customer aggregate (a User) BY IDENTITY. Notice the
// order package does NOT import the user package: aggregates reference each
// other only by opaque identifier, never by holding the other aggregate's
// object. The application layer bridges the two when it needs to enforce a
// cross-aggregate rule (e.g. "the customer must exist and be active").
type CustomerID struct{ value uuid.UUID }

func CustomerIDFromString(s string) (CustomerID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return CustomerID{}, ErrInvalidCustomer
	}
	return CustomerID{value: u}, nil
}

func (c CustomerID) String() string { return c.value.String() }
func (c CustomerID) IsZero() bool   { return c.value == uuid.Nil }
