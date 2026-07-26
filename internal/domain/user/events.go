package user

import (
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// User domain events. They embed shared.BaseEvent so they satisfy shared.Event
// and can travel on the shared event bus alongside other aggregates' events.
//
// Unlike order and account events they deliberately do NOT implement
// shared.Keyed: nothing reprojects a read model from them, and shared.Keyed is
// an optional interface that consumers discover by type assertion. Keep it that
// way — implementing it "for symmetry" would make user events look routable to
// the outbox publisher and the order projector when they are not.

// Registered is emitted when a new user is created.
type Registered struct {
	shared.BaseEvent
	UserID ID
	Email  Email
}

func (Registered) EventName() string { return "user.registered" }

// Activated is emitted when a user's account is activated.
type Activated struct {
	shared.BaseEvent
	UserID ID
}

func (Activated) EventName() string { return "user.activated" }

// Deactivated is emitted when a user's account is deactivated. Other aggregates
// (e.g. Order) may react to it to keep the system eventually consistent.
type Deactivated struct {
	shared.BaseEvent
	UserID ID
}

func (Deactivated) EventName() string { return "user.deactivated" }
