package account

import "github.com/example/go-ddd-hex/internal/domain/shared"

// ID is the identity of an Account aggregate.
//
// It is an alias of the shared generic identity, instantiated with the aggregate
// itself as phantom type. The whole identity API (String, Equals, IsZero) lives
// once in shared.ID; all this context declares is which aggregate the identity
// belongs to and which error a malformed one maps to.
type ID = shared.ID[Account]

// NewID generates a fresh identity.
func NewID() ID { return shared.NewID[Account]() }

// IDFromString parses an identity, translating the shared parse failure into
// this context's own sentinel.
func IDFromString(s string) (ID, error) {
	id, err := shared.ParseID[Account](s)
	if err != nil {
		return ID{}, ErrNotFound
	}
	return id, nil
}
