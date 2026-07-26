package memory

import (
	"context"
	"sort"
	"sync"

	order "github.com/example/go-ddd-hex/internal/domain/order"
)

// OrderRepository is a goroutine-safe, in-memory Order store.
type OrderRepository struct {
	mu   sync.RWMutex
	byID map[string]*order.Order
}

func NewOrderRepository() *OrderRepository {
	return &OrderRepository{byID: make(map[string]*order.Order)}
}

var _ order.Repository = (*OrderRepository)(nil)

func (r *OrderRepository) Save(_ context.Context, o *order.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[o.ID().String()] = cloneOrder(o)
	return nil
}

func (r *OrderRepository) FindByID(_ context.Context, id order.ID) (*order.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.byID[id.String()]
	if !ok {
		return nil, order.ErrNotFound
	}
	return cloneOrder(o), nil
}

func (r *OrderRepository) ListByCustomer(_ context.Context, customerID order.CustomerID) ([]*order.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Non-nil so an empty result serialises as [] and not null, matching the
	// Postgres adapter (see internal/adapters/contract).
	out := make([]*order.Order, 0)
	for _, o := range r.byID {
		if o.CustomerID().String() == customerID.String() {
			out = append(out, cloneOrder(o))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt().After(out[j].CreatedAt()) })
	return out, nil
}

func (r *OrderRepository) List(_ context.Context, limit, offset int) ([]*order.Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]*order.Order, 0, len(r.byID))
	for _, o := range r.byID {
		all = append(all, o)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt().After(all[j].CreatedAt()) })
	if offset >= len(all) {
		return []*order.Order{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	page := make([]*order.Order, 0, end-offset)
	for _, o := range all[offset:end] {
		page = append(page, cloneOrder(o))
	}
	return page, nil
}

func (r *OrderRepository) Delete(_ context.Context, id order.ID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id.String()]; !ok {
		return order.ErrNotFound
	}
	delete(r.byID, id.String())
	return nil
}

func cloneOrder(o *order.Order) *order.Order {
	return order.Rehydrate(o.ID(), o.CustomerID(), o.Currency(), o.Status(), o.Lines(), o.CreatedAt(), o.UpdatedAt())
}
