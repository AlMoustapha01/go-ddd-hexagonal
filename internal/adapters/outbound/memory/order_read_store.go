package memory

import (
	"context"
	"sort"
	"sync"

	apporder "github.com/example/go-ddd-hex/internal/application/order"
	domainorder "github.com/example/go-ddd-hex/internal/domain/order"
)

// OrderReadStore is the in-memory CQRS read model for orders. It is a store
// SEPARATE from OrderRepository (the write side): the projector writes here, the
// query service reads here, and the two never share state with the aggregate
// store.
type OrderReadStore struct {
	mu   sync.RWMutex
	byID map[string]apporder.DetailView
}

// NewOrderReadStore builds an empty read store.
func NewOrderReadStore() *OrderReadStore {
	return &OrderReadStore{byID: make(map[string]apporder.DetailView)}
}

var _ apporder.ReadStore = (*OrderReadStore)(nil)

// Upsert inserts or replaces a projection.
func (s *OrderReadStore) Upsert(_ context.Context, d apporder.DetailView) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[d.ID] = d
	return nil
}

// Delete removes a projection.
func (s *OrderReadStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
	return nil
}

// Get returns a detail projection or domain order.ErrNotFound.
func (s *OrderReadStore) Get(_ context.Context, id string) (apporder.DetailView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.byID[id]
	if !ok {
		return apporder.DetailView{}, domainorder.ErrNotFound
	}
	return d, nil
}

// List returns summaries, newest first, paginated.
func (s *OrderReadStore) List(_ context.Context, limit, offset int) ([]apporder.SummaryView, error) {
	all := s.sortedSummaries(func(apporder.DetailView) bool { return true })
	if offset >= len(all) {
		return []apporder.SummaryView{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

// ListByCustomer returns a customer's summaries, newest first.
func (s *OrderReadStore) ListByCustomer(_ context.Context, customerID string) ([]apporder.SummaryView, error) {
	return s.sortedSummaries(func(d apporder.DetailView) bool { return d.CustomerID == customerID }), nil
}

func (s *OrderReadStore) sortedSummaries(keep func(apporder.DetailView) bool) []apporder.SummaryView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]apporder.SummaryView, 0, len(s.byID))
	for _, d := range s.byID {
		if keep(d) {
			out = append(out, d.Summary())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}
