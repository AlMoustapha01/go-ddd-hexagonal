package account

import "context"

// UseCases is the INBOUND PORT for account commands.
type UseCases interface {
	Open(ctx context.Context, cmd OpenCommand) (AccountView, error)
	Deposit(ctx context.Context, cmd AmountCommand) (AccountView, error)
	Withdraw(ctx context.Context, cmd AmountCommand) (AccountView, error)
}

// Queries is the INBOUND PORT for account reads. In this event-sourced context
// reads are served by replaying the stream (state = fold(events)) rather than
// from a stored row; History exposes the raw event log.
type Queries interface {
	Get(ctx context.Context, id string) (AccountView, error)
	History(ctx context.Context, id string) ([]HistoryEntry, error)
}
