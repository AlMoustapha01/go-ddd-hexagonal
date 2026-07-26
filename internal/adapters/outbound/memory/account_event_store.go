package memory

import (
	"context"
	"sync"

	account "github.com/example/go-ddd-hex/internal/domain/account"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// AccountEventStore is an in-memory event store for the event-sourced Account
// aggregate. Each aggregate id maps to an append-only, ordered stream.
type AccountEventStore struct {
	mu      sync.RWMutex
	streams map[string][]shared.Event
}

// NewAccountEventStore builds an empty event store.
func NewAccountEventStore() *AccountEventStore {
	return &AccountEventStore{streams: make(map[string][]shared.Event)}
}

var _ account.EventStore = (*AccountEventStore)(nil)

// Load returns a copy of the stream, or ErrNotFound if it does not exist.
func (s *AccountEventStore) Load(_ context.Context, id account.ID) ([]shared.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stream, ok := s.streams[id.String()]
	if !ok || len(stream) == 0 {
		return nil, account.ErrNotFound
	}
	out := make([]shared.Event, len(stream))
	copy(out, stream)
	return out, nil
}

// Append enforces optimistic concurrency: it only appends if the current stream
// length equals expectedVersion.
func (s *AccountEventStore) Append(_ context.Context, id account.ID, expectedVersion int, events []shared.Event) error {
	if len(events) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := id.String()
	if len(s.streams[key]) != expectedVersion {
		return account.ErrConcurrencyConflict
	}
	s.streams[key] = append(s.streams[key], events...)
	return nil
}
