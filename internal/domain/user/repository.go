package user

import "context"

// Repository is an OUTBOUND PORT. It is declared in the domain but implemented
// by outbound adapters (in-memory, Postgres, ...). The domain defines what it
// needs; adapters provide the how. This inversion is what keeps the domain
// free of infrastructure.
type Repository interface {
	// Save inserts or updates the aggregate.
	Save(ctx context.Context, u *User) error
	// FindByID returns ErrNotFound if no user matches.
	FindByID(ctx context.Context, id ID) (*User, error)
	// FindByEmail returns ErrNotFound if no user matches.
	FindByEmail(ctx context.Context, email Email) (*User, error)
	// ExistsByEmail reports whether an email is already registered.
	ExistsByEmail(ctx context.Context, email Email) (bool, error)
	// List returns users with simple pagination.
	List(ctx context.Context, limit, offset int) ([]*User, error)
	// Delete removes a user by ID.
	Delete(ctx context.Context, id ID) error
}
