// Package user (application) holds the use cases for the User aggregate.
//
// The application layer orchestrates the domain: it loads aggregates through
// ports, invokes domain behavior, persists the result, and dispatches events.
// It contains NO business rules itself (those live in the domain) and NO
// infrastructure detail (that lives in adapters). It depends only on the domain
// package and on ports.
package user

import (
	"context"
	"errors"
	"log/slog"

	"github.com/example/go-ddd-hex/internal/domain/shared"
	domain "github.com/example/go-ddd-hex/internal/domain/user"
)

// Service implements the inbound UseCases port.
type Service struct {
	repo   domain.Repository
	events shared.EventPublisher
	log    *slog.Logger
}

// NewService wires the use cases with their outbound ports (dependency
// injection). Nothing here references a concrete adapter.
func NewService(repo domain.Repository, events shared.EventPublisher, log *slog.Logger) *Service {
	return &Service{repo: repo, events: events, log: log}
}

// Compile-time assertion that Service satisfies the inbound port.
var _ UseCases = (*Service)(nil)

func (s *Service) Register(ctx context.Context, cmd RegisterCommand) (UserView, error) {
	email, err := domain.NewEmail(cmd.Email)
	if err != nil {
		return UserView{}, err
	}

	exists, err := s.repo.ExistsByEmail(ctx, email)
	if err != nil {
		return UserView{}, err
	}
	if exists {
		return UserView{}, domain.ErrEmailTaken
	}

	u, err := domain.Register(cmd.Name, email)
	if err != nil {
		return UserView{}, err
	}
	if err := s.repo.Save(ctx, u); err != nil {
		return UserView{}, err
	}
	s.dispatch(ctx, u)
	return toView(u), nil
}

func (s *Service) Activate(ctx context.Context, id string) (UserView, error) {
	return s.mutate(ctx, id, func(u *domain.User) error { return u.Activate() })
}

func (s *Service) Deactivate(ctx context.Context, id string) (UserView, error) {
	return s.mutate(ctx, id, func(u *domain.User) error { return u.Deactivate() })
}

func (s *Service) Rename(ctx context.Context, cmd RenameCommand) (UserView, error) {
	return s.mutate(ctx, cmd.ID, func(u *domain.User) error { return u.Rename(cmd.Name) })
}

func (s *Service) GetByID(ctx context.Context, id string) (UserView, error) {
	uid, err := domain.IDFromString(id)
	if err != nil {
		return UserView{}, err
	}
	u, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return UserView{}, err
	}
	return toView(u), nil
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]UserView, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	users, err := s.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	views := make([]UserView, 0, len(users))
	for _, u := range users {
		views = append(views, toView(u))
	}
	return views, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	uid, err := domain.IDFromString(id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, uid)
}

// mutate is a small helper for load → apply domain behavior → persist → dispatch.
func (s *Service) mutate(ctx context.Context, id string, apply func(*domain.User) error) (UserView, error) {
	uid, err := domain.IDFromString(id)
	if err != nil {
		return UserView{}, err
	}
	u, err := s.repo.FindByID(ctx, uid)
	if err != nil {
		return UserView{}, err
	}
	if err := apply(u); err != nil {
		return UserView{}, err
	}
	if err := s.repo.Save(ctx, u); err != nil {
		return UserView{}, err
	}
	s.dispatch(ctx, u)
	return toView(u), nil
}

// dispatch publishes recorded domain events. A failure to publish must not
// invalidate an already-persisted change, so it is logged, not returned.
func (s *Service) dispatch(ctx context.Context, u *domain.User) {
	events := u.PullEvents()
	if len(events) == 0 || s.events == nil {
		return
	}
	if err := s.events.Publish(ctx, events...); err != nil {
		s.log.WarnContext(ctx, "failed to publish domain events", slog.Any("error", err))
	}
}

// IsValidationError reports whether an error is a client-side (400-class)
// domain validation error, as opposed to a not-found or infrastructure error.
func IsValidationError(err error) bool {
	switch {
	case errors.Is(err, domain.ErrEmailEmpty),
		errors.Is(err, domain.ErrEmailInvalid),
		errors.Is(err, domain.ErrNameEmpty),
		errors.Is(err, domain.ErrAlreadyActive),
		errors.Is(err, domain.ErrAlreadyInactive):
		return true
	default:
		return false
	}
}
