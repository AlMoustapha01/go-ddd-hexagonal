package memory

import (
	"context"
	"sync"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// OutboxStore is an in-memory shared.OutboxStore for local dev and tests.
//
// NOTE: with the in-memory driver the "transaction" is a no-op (see
// Transactor), so the atomicity guarantee that makes the Outbox pattern correct
// only truly holds with the Postgres driver. This implementation still
// exercises the full write → relay → publish flow.
type OutboxStore struct {
	mu      sync.Mutex
	pending []shared.OutboxRecord
}

// NewOutboxStore builds an empty outbox.
func NewOutboxStore() *OutboxStore {
	return &OutboxStore{}
}

var _ shared.OutboxStore = (*OutboxStore)(nil)

// Add appends records to the outbox.
func (s *OutboxStore) Add(_ context.Context, records ...shared.OutboxRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, records...)
	return nil
}

// FetchUnpublished returns up to limit pending records, oldest first.
func (s *OutboxStore) FetchUnpublished(_ context.Context, limit int) ([]shared.OutboxRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.pending) {
		limit = len(s.pending)
	}
	out := make([]shared.OutboxRecord, limit)
	copy(out, s.pending[:limit])
	return out, nil
}

// MarkPublished removes the given records from the pending set.
func (s *OutboxStore) MarkPublished(_ context.Context, ids ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	done := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		done[id] = struct{}{}
	}
	kept := s.pending[:0]
	for _, rec := range s.pending {
		if _, ok := done[rec.ID]; !ok {
			kept = append(kept, rec)
		}
	}
	s.pending = kept
	return nil
}
