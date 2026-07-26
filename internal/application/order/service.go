// Package order (application) holds the use cases for the Order aggregate.
package order

import (
	"context"
	"errors"
	"log/slog"

	domain "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// Service implements the inbound order UseCases port (the COMMAND side).
//
// Every command persists the aggregate AND its events atomically through the
// Outbox pattern: repo.Save and outbox.Publish run inside a single Unit of Work
// (uow.Within). After the transaction commits, the service asks the dispatcher
// to relay the outbox immediately (a low-latency hint); the relay's background
// poller is the durability backstop. This removes the dual-write hazard —
// events can never be lost or emitted for a rolled-back change.
type Service struct {
	repo       domain.Repository
	customers  CustomerChecker
	uow        shared.Atomic
	outbox     shared.EventPublisher
	dispatcher Dispatcher
	log        *slog.Logger
}

// NewService wires the command service with its ports: write repository,
// customer checker, unit of work, outbox publisher and the relay dispatcher.
func NewService(repo domain.Repository, customers CustomerChecker, uow shared.Atomic, outbox shared.EventPublisher, dispatcher Dispatcher, log *slog.Logger) *Service {
	return &Service{repo: repo, customers: customers, uow: uow, outbox: outbox, dispatcher: dispatcher, log: log}
}

var _ UseCases = (*Service)(nil)

// ErrCustomerNotActive is returned when the referenced customer is missing or
// inactive — a cross-aggregate rule checked here in the application layer.
var ErrCustomerNotActive = errors.New("order: customer does not exist or is not active")

func (s *Service) Create(ctx context.Context, cmd CreateCommand) (OrderView, error) {
	// Cross-aggregate consistency: verify the customer via the narrow port
	// (a read, kept outside the write transaction).
	active, err := s.customers.IsActiveCustomer(ctx, cmd.CustomerID)
	if err != nil {
		return OrderView{}, err
	}
	if !active {
		return OrderView{}, ErrCustomerNotActive
	}

	customerID, err := domain.CustomerIDFromString(cmd.CustomerID)
	if err != nil {
		return OrderView{}, err
	}
	o, err := domain.NewDraft(customerID, cmd.Currency)
	if err != nil {
		return OrderView{}, err
	}
	if err := s.commit(ctx, o); err != nil {
		return OrderView{}, err
	}
	return toView(o), nil
}

func (s *Service) AddLine(ctx context.Context, cmd AddLineCommand) (OrderView, error) {
	return s.mutate(ctx, cmd.OrderID, func(o *domain.Order) error {
		price, err := domain.NewMoney(cmd.UnitPriceMinor, cmd.Currency)
		if err != nil {
			return err
		}
		return o.AddLine(cmd.ProductID, price, cmd.Quantity)
	})
}

func (s *Service) Place(ctx context.Context, id string) (OrderView, error) {
	return s.mutate(ctx, id, func(o *domain.Order) error { return o.Place() })
}

func (s *Service) Pay(ctx context.Context, id string) (OrderView, error) {
	return s.mutate(ctx, id, func(o *domain.Order) error { return o.Pay() })
}

func (s *Service) Cancel(ctx context.Context, id, reason string) (OrderView, error) {
	return s.mutate(ctx, id, func(o *domain.Order) error { return o.Cancel(reason) })
}

func (s *Service) mutate(ctx context.Context, id string, apply func(*domain.Order) error) (OrderView, error) {
	oid, err := domain.IDFromString(id)
	if err != nil {
		return OrderView{}, err
	}
	o, err := s.repo.FindByID(ctx, oid)
	if err != nil {
		return OrderView{}, err
	}
	if err := apply(o); err != nil {
		return OrderView{}, err
	}
	if err := s.commit(ctx, o); err != nil {
		return OrderView{}, err
	}
	return toView(o), nil
}

// commit persists the aggregate and its events in one transaction (Outbox
// pattern), then hints the relay to publish them.
func (s *Service) commit(ctx context.Context, o *domain.Order) error {
	err := s.uow.Within(ctx, func(ctx context.Context) error {
		if err := s.repo.Save(ctx, o); err != nil {
			return err
		}
		// Written in the SAME transaction as the aggregate.
		return s.outbox.Publish(ctx, o.PullEvents()...)
	})
	if err != nil {
		return err
	}

	// Post-commit: nudge the relay to deliver now (best-effort, low latency).
	// If this fails or the process dies here, the relay's poller delivers later.
	if s.dispatcher != nil {
		if err := s.dispatcher.Dispatch(ctx); err != nil {
			s.log.WarnContext(ctx, "inline outbox dispatch failed (poller will retry)", slog.Any("error", err))
		}
	}
	return nil
}

// IsValidationError reports client-side (400-class) order errors.
func IsValidationError(err error) bool {
	switch {
	case errors.Is(err, domain.ErrInvalidCurrency),
		errors.Is(err, domain.ErrNegativeAmount),
		errors.Is(err, domain.ErrCurrencyMismatch),
		errors.Is(err, domain.ErrEmptyProduct),
		errors.Is(err, domain.ErrInvalidQuantity),
		errors.Is(err, domain.ErrNoLines),
		errors.Is(err, domain.ErrNotDraft),
		errors.Is(err, domain.ErrNotPlaced),
		errors.Is(err, domain.ErrAlreadyFinalized),
		errors.Is(err, domain.ErrLineCurrency),
		errors.Is(err, domain.ErrInvalidCustomer),
		errors.Is(err, ErrCustomerNotActive):
		return true
	default:
		return false
	}
}
