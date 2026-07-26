package user

import (
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// User domain events. They embed shared.BaseEvent so they satisfy shared.Event
// and can travel on the shared event bus alongside other aggregates' events.

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

func newBase(at time.Time) shared.BaseEvent { return shared.BaseEvent{At: at} }
