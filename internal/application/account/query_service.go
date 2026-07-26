package account

import (
	"context"

	domain "github.com/example/go-ddd-hex/internal/domain/account"
)

// QueryService serves reads by replaying the event stream. For simple state
// this "fold on read" approach is fine; for heavy read workloads you would
// maintain a projected read model (as the Order context does with CQRS) fed by
// the same events.
type QueryService struct {
	store domain.EventStore
}

// NewQueryService builds the read side over the event store.
func NewQueryService(store domain.EventStore) *QueryService {
	return &QueryService{store: store}
}

var _ Queries = (*QueryService)(nil)

// Get returns the current state by folding the stream.
func (q *QueryService) Get(ctx context.Context, id string) (AccountView, error) {
	aid, err := domain.IDFromString(id)
	if err != nil {
		return AccountView{}, err
	}
	events, err := q.store.Load(ctx, aid)
	if err != nil {
		return AccountView{}, err
	}
	return toView(domain.Rehydrate(events)), nil
}

// History returns the full event log — a built-in audit trail, one of event
// sourcing's headline benefits.
func (q *QueryService) History(ctx context.Context, id string) ([]HistoryEntry, error) {
	aid, err := domain.IDFromString(id)
	if err != nil {
		return nil, err
	}
	events, err := q.store.Load(ctx, aid)
	if err != nil {
		return nil, err
	}
	entries := make([]HistoryEntry, 0, len(events))
	for i, e := range events {
		entries = append(entries, HistoryEntry{
			Sequence:    i + 1,
			Event:       e.EventName(),
			AmountMinor: amountOf(e),
			OccurredAt:  e.OccurredAt(),
		})
	}
	return entries, nil
}

func amountOf(e any) int64 {
	switch ev := e.(type) {
	case domain.Deposited:
		return ev.Amount
	case domain.Withdrawn:
		return ev.Amount
	default:
		return 0
	}
}
