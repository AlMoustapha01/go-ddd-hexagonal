package order

import "github.com/example/go-ddd-hex/internal/domain/shared"

// ID is the identity of an Order aggregate.
//
// It is an alias of the shared generic identity, instantiated with the aggregate
// itself as phantom type. The whole identity API (String, Equals, IsZero) lives
// once in shared.ID; all this context declares is which aggregate the identity
// belongs to and which error a malformed one maps to.
type ID = shared.ID[Order]

// NewID generates a fresh identity.
func NewID() ID { return shared.NewID[Order]() }

// IDFromString parses an identity, translating the shared parse failure into
// this context's own sentinel.
func IDFromString(s string) (ID, error) {
	id, err := shared.ParseID[Order](s)
	if err != nil {
		return ID{}, ErrNotFound
	}
	return id, nil
}

// customer is a phantom marker with no runtime existence. The order context has
// no Customer type of its own — and must not import the user package to borrow
// one — so it declares this tag purely to give CustomerID a type the compiler
// keeps distinct from order.ID.
type customer struct{}

// CustomerID references the customer aggregate (a User) BY IDENTITY. Notice the
// order package does NOT import the user package: aggregates reference each
// other only by opaque identifier, never by holding the other aggregate's
// object. The application layer bridges the two when it needs to enforce a
// cross-aggregate rule (e.g. "the customer must exist and be active").
type CustomerID = shared.ID[customer]

// CustomerIDFromString parses a customer reference.
func CustomerIDFromString(s string) (CustomerID, error) {
	id, err := shared.ParseID[customer](s)
	if err != nil {
		return CustomerID{}, ErrInvalidCustomer
	}
	return id, nil
}
