package shared

import (
	"errors"

	"github.com/google/uuid"
)

// ErrInvalidID is returned by ParseID when the input is not a valid UUID.
//
// Bounded contexts normally never surface it: they map it onto their own
// sentinel (order.ErrNotFound, order.ErrInvalidCustomer, ...) so that callers
// keep inspecting context-specific errors with errors.Is. It exists so that a
// new context cloned from this boilerplate has a sensible default before it has
// defined its own errors.
var ErrInvalidID = errors.New("shared: invalid identifier")

// ID is the identity value object every aggregate builds on. It is immutable:
// the underlying UUID is unexported, so an ID can only come from NewID or
// ParseID and can never be tampered with from outside this package.
//
// The type parameter T is a PHANTOM TYPE — it never appears in the struct body
// and no value of T is ever created. It exists for one reason: to make the
// compiler treat ID[Order] and ID[User] as two incompatible types. A single
// non-generic shared ID would happily let an order id be passed where a user id
// is expected, which is precisely the class of bug typed identities exist to
// prevent.
//
// Each bounded context declares its own alias:
//
//	type ID = shared.ID[Order]
//
// An alias (=) rather than a defined type, because a defined type would NOT
// inherit the methods below and every context would be back to redeclaring
// String/Equals/IsZero by hand — the duplication this type removes.
type ID[T any] struct{ value uuid.UUID }

// NewID generates a fresh identity for the aggregate T.
//
// It mints a UUID version 7: the first 48 bits are the Unix timestamp in
// milliseconds, so identities sort chronologically. That matters at the storage
// layer — every aggregate here is keyed by its id, and a time-ordered primary
// key makes inserts land on the rightmost page of the B-tree index instead of
// scattering across it, which is what random (v4) identifiers do.
//
// Trade-off to be aware of: a v7 identity PUBLISHES ITS CREATION TIME to anyone
// who can read it. If ids ever leak into URLs or API responses where the
// creation instant is sensitive, that is a real disclosure — switch back to
// uuid.New() (v4) here and the whole domain follows.
//
// Must is not a new failure mode: uuid.New() is itself Must(NewRandom()), so
// this code already panicked if crypto/rand failed.
func NewID[T any]() ID[T] { return ID[T]{value: uuid.Must(uuid.NewV7())} }

// ParseID parses the string form of an identity, returning ErrInvalidID when the
// input is not a UUID.
func ParseID[T any](s string) (ID[T], error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return ID[T]{}, ErrInvalidID
	}
	return ID[T]{value: u}, nil
}

// String returns the canonical string form of the identity.
func (id ID[T]) String() string { return id.value.String() }

// Equals compares two identities by value. Thanks to the phantom type it only
// accepts an identity of the SAME aggregate.
func (id ID[T]) Equals(o ID[T]) bool { return id.value == o.value }

// IsZero reports whether the identity is the zero value (never constructed).
func (id ID[T]) IsZero() bool { return id.value == uuid.Nil }
