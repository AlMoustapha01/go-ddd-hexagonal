package order

import "context"

// Repository is the OUTBOUND PORT for persisting Order aggregates.
type Repository interface {
	Save(ctx context.Context, o *Order) error
	FindByID(ctx context.Context, id ID) (*Order, error)
	// ListByCustomer returns a customer's orders, newest first.
	ListByCustomer(ctx context.Context, customerID CustomerID) ([]*Order, error)
	List(ctx context.Context, limit, offset int) ([]*Order, error)
	Delete(ctx context.Context, id ID) error
}
