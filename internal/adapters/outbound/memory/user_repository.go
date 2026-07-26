// Package memory provides an in-memory implementation of the user.Repository
// outbound port. It is ideal for unit tests and local development, and proves
// the port abstraction: the application layer cannot tell it apart from the
// Postgres adapter.
package memory

import (
	"context"
	"sort"
	"sync"

	domain "github.com/example/go-ddd-hex/internal/domain/user"
)

// UserRepository is a goroutine-safe, in-memory store.
type UserRepository struct {
	mu   sync.RWMutex
	byID map[string]*domain.User
}

func NewUserRepository() *UserRepository {
	return &UserRepository{byID: make(map[string]*domain.User)}
}

var _ domain.Repository = (*UserRepository)(nil)

func (r *UserRepository) Save(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Store a snapshot so external mutations don't corrupt the store.
	r.byID[u.ID().String()] = clone(u)
	return nil
}

func (r *UserRepository) FindByID(_ context.Context, id domain.ID) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.byID[id.String()]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(u), nil
}

func (r *UserRepository) FindByEmail(_ context.Context, email domain.Email) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, u := range r.byID {
		if u.Email().Equals(email) {
			return clone(u), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *UserRepository) ExistsByEmail(_ context.Context, email domain.Email) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, u := range r.byID {
		if u.Email().Equals(email) {
			return true, nil
		}
	}
	return false, nil
}

func (r *UserRepository) List(_ context.Context, limit, offset int) ([]*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]*domain.User, 0, len(r.byID))
	for _, u := range r.byID {
		all = append(all, u)
	}
	// Deterministic ordering: newest first.
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt().After(all[j].CreatedAt())
	})

	if offset >= len(all) {
		return []*domain.User{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	page := make([]*domain.User, 0, end-offset)
	for _, u := range all[offset:end] {
		page = append(page, clone(u))
	}
	return page, nil
}

func (r *UserRepository) Delete(_ context.Context, id domain.ID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id.String()]; !ok {
		return domain.ErrNotFound
	}
	delete(r.byID, id.String())
	return nil
}

// clone rebuilds an aggregate from its state so the store never hands out a
// pointer a caller could mutate in place.
func clone(u *domain.User) *domain.User {
	return domain.Rehydrate(u.ID(), u.Name(), u.Email(), u.IsActive(), u.CreatedAt(), u.UpdatedAt())
}
