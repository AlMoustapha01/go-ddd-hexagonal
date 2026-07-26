package user

import "context"

// UseCases is the INBOUND PORT: the contract that driving adapters (HTTP, CLI,
// gRPC, tests) call. They depend on this interface, not on the concrete
// Service, which keeps the delivery mechanism swappable.
type UseCases interface {
	Register(ctx context.Context, cmd RegisterCommand) (UserView, error)
	Activate(ctx context.Context, id string) (UserView, error)
	Deactivate(ctx context.Context, id string) (UserView, error)
	Rename(ctx context.Context, cmd RenameCommand) (UserView, error)
	GetByID(ctx context.Context, id string) (UserView, error)
	List(ctx context.Context, limit, offset int) ([]UserView, error)
	Delete(ctx context.Context, id string) error
}
