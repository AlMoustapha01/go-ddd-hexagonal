package account

import "github.com/google/uuid"

// ID is the identity of an Account aggregate.
type ID struct{ value uuid.UUID }

// NewID generates a fresh identity.
func NewID() ID { return ID{value: uuid.New()} }

// IDFromString parses an identity.
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
