// Package user is the domain layer for the User aggregate.
//
// It contains pure business logic and has NO dependency on frameworks,
// databases, transport, or any outer layer. Everything the outside world needs
// is expressed here as interfaces (ports); the outside world depends on this
// package, never the reverse. This is the "domain" at the center of the
// hexagon.
package user

import (
	"strings"
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// User is the aggregate root. It protects its invariants: state can only be
// changed through its methods, never by mutating fields from outside the
// package (they are unexported). Callers reconstruct persisted users via
// Rehydrate.
type User struct {
	id        ID
	name      string
	email     Email
	active    bool
	createdAt time.Time
	updatedAt time.Time

	events []shared.Event
}

// Register is the factory for a brand-new user. It enforces invariants and
// records a domain event. A newly registered user starts inactive.
func Register(name string, email Email) (*User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameEmpty
	}
	if email.IsZero() {
		return nil, ErrEmailEmpty
	}
	now := time.Now().UTC()
	u := &User{
		id:        NewID(),
		name:      name,
		email:     email,
		active:    false,
		createdAt: now,
		updatedAt: now,
	}
	u.record(Registered{BaseEvent: shared.EventAt(now), UserID: u.id, Email: email})
	return u, nil
}

// Rehydrate reconstructs a User from persisted state WITHOUT running factory
// invariants or emitting events. Only outbound persistence adapters should call
// it, when mapping a database row back into the aggregate.
func Rehydrate(id ID, name string, email Email, active bool, createdAt, updatedAt time.Time) *User {
	return &User{
		id:        id,
		name:      name,
		email:     email,
		active:    active,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}
}

// Activate transitions the account to active, enforcing the invariant that an
// already-active account cannot be re-activated.
func (u *User) Activate() error {
	if u.active {
		return ErrAlreadyActive
	}
	u.active = true
	u.touch()
	u.record(Activated{BaseEvent: shared.EventAt(u.updatedAt), UserID: u.id})
	return nil
}

// Deactivate transitions the account to inactive.
func (u *User) Deactivate() error {
	if !u.active {
		return ErrAlreadyInactive
	}
	u.active = false
	u.touch()
	u.record(Deactivated{BaseEvent: shared.EventAt(u.updatedAt), UserID: u.id})
	return nil
}

// Rename changes the display name, enforcing the non-empty invariant.
func (u *User) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNameEmpty
	}
	u.name = name
	u.touch()
	return nil
}

// Getters expose read-only state. There are deliberately no setters.
func (u *User) ID() ID               { return u.id }
func (u *User) Name() string         { return u.name }
func (u *User) Email() Email         { return u.email }
func (u *User) IsActive() bool       { return u.active }
func (u *User) CreatedAt() time.Time { return u.createdAt }
func (u *User) UpdatedAt() time.Time { return u.updatedAt }

// PullEvents returns the recorded domain events and clears the buffer. The
// application layer calls this after persisting to dispatch the events.
func (u *User) PullEvents() []shared.Event {
	events := u.events
	u.events = nil
	return events
}

func (u *User) record(e shared.Event) { u.events = append(u.events, e) }
func (u *User) touch()                { u.updatedAt = time.Now().UTC() }
