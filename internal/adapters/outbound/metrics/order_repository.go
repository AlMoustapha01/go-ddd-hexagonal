package metrics

import (
	"context"
	"errors"
	"time"

	order "github.com/example/go-ddd-hex/internal/domain/order"
	appmetrics "github.com/example/go-ddd-hex/internal/platform/metrics"
)

// orderRepositoryPort is the label identifying this port in the metrics.
const orderRepositoryPort = "order.Repository"

// OrderRepository decorates an order.Repository with latency and error metrics.
//
// It is the one port instrumented here, as a demonstration: the same shape
// applies unchanged to any other port — copy the file, swap the interface.
// Duplicating it across every repository would double the code and teach
// nothing new, so it is deliberately left to whoever needs it.
//
// Why measure here at all, when the HTTP histogram already times the request?
// Because that histogram cannot attribute latency. When p99 rises, this is what
// says whether the database is the cause.
type OrderRepository struct {
	inner   order.Repository
	metrics *appmetrics.Metrics
}

// NewOrderRepository decorates an order repository with instrumentation.
func NewOrderRepository(inner order.Repository, m *appmetrics.Metrics) *OrderRepository {
	return &OrderRepository{inner: inner, metrics: m}
}

var _ order.Repository = (*OrderRepository)(nil)

func (r *OrderRepository) Save(ctx context.Context, o *order.Order) error {
	return r.observe("Save", func() error { return r.inner.Save(ctx, o) })
}

func (r *OrderRepository) FindByID(ctx context.Context, id order.ID) (*order.Order, error) {
	var out *order.Order
	err := r.observe("FindByID", func() error {
		var err error
		out, err = r.inner.FindByID(ctx, id)
		return err
	})
	return out, err
}

func (r *OrderRepository) ListByCustomer(ctx context.Context, customerID order.CustomerID) ([]*order.Order, error) {
	var out []*order.Order
	err := r.observe("ListByCustomer", func() error {
		var err error
		out, err = r.inner.ListByCustomer(ctx, customerID)
		return err
	})
	return out, err
}

func (r *OrderRepository) List(ctx context.Context, limit, offset int) ([]*order.Order, error) {
	var out []*order.Order
	err := r.observe("List", func() error {
		var err error
		out, err = r.inner.List(ctx, limit, offset)
		return err
	})
	return out, err
}

func (r *OrderRepository) Delete(ctx context.Context, id order.ID) error {
	return r.observe("Delete", func() error { return r.inner.Delete(ctx, id) })
}

// observe times a call and counts its failures, returning the inner result
// untouched — including the error itself, so callers upstream keep matching it
// with errors.Is.
func (r *OrderRepository) observe(op string, call func() error) error {
	start := time.Now()
	err := call()
	r.metrics.RepoDuration.WithLabelValues(orderRepositoryPort, op).Observe(time.Since(start).Seconds())

	// ErrNotFound is an ordinary outcome, not a failure: a lookup that finds
	// nothing is the store working correctly. Counting it would inflate the
	// error rate to the point of making the metric useless for alerting.
	if err != nil && !errors.Is(err, order.ErrNotFound) {
		r.metrics.RepoErrors.WithLabelValues(orderRepositoryPort, op).Inc()
	}
	return err
}
