package integration

import (
	"context"
	"errors"

	user "github.com/example/go-ddd-hex/internal/domain/user"
)

// UserCustomerChecker implements orderapp.CustomerChecker on top of the user
// repository. It is the anti-corruption layer between the two contexts: the
// order use cases depend on the narrow CustomerChecker interface, and this
// adapter translates that into a user-context lookup. The order context stays
// unaware of the user aggregate's shape.
type UserCustomerChecker struct {
	users user.Repository
}

func NewUserCustomerChecker(users user.Repository) *UserCustomerChecker {
	return &UserCustomerChecker{users: users}
}

// IsActiveCustomer reports whether the id maps to an existing, active user.
func (c *UserCustomerChecker) IsActiveCustomer(ctx context.Context, customerID string) (bool, error) {
	id, err := user.IDFromString(customerID)
	if err != nil {
		return false, nil // malformed id => not a valid customer
	}
	u, err := c.users.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return u.IsActive(), nil
}
