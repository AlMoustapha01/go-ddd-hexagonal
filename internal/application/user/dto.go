package user

import (
	"time"

	domain "github.com/example/go-ddd-hex/internal/domain/user"
)

// Commands are the inputs to use cases. They are plain data, decoupled from any
// transport (HTTP, gRPC, CLI ...).
type RegisterCommand struct {
	Name  string
	Email string
}

type RenameCommand struct {
	ID   string
	Name string
}

// UserView is the read model returned by use cases. It is a flat, safe
// projection of the aggregate — the domain object itself never leaks outward.
type UserView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toView(u *domain.User) UserView {
	return UserView{
		ID:        u.ID().String(),
		Name:      u.Name(),
		Email:     u.Email().String(),
		Active:    u.IsActive(),
		CreatedAt: u.CreatedAt(),
		UpdatedAt: u.UpdatedAt(),
	}
}
