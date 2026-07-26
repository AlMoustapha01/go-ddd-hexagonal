package user

import "github.com/example/go-ddd-hex/internal/domain/shared"

// ID is the identity of a User aggregate.
//
// It is an alias of the shared generic identity, instantiated with the aggregate
// itself as phantom type. The whole identity API (String, Equals, IsZero) lives
// once in shared.ID; all this context declares is which aggregate the identity
// belongs to and which error a malformed one maps to.
type ID = shared.ID[User]

// NewID generates a fresh identity.
func NewID() ID { return shared.NewID[User]() }

// IDFromString parses an identity from its string form, translating the shared
// parse failure into this context's own sentinel so callers keep using
// errors.Is(err, user.ErrNotFound).
func IDFromString(s string) (ID, error) {
	id, err := shared.ParseID[User](s)
	if err != nil {
		return ID{}, ErrNotFound
	}
	return id, nil
}
