package order

import "context"

// Queries is the INBOUND PORT for the read side. Driving adapters (HTTP GET
// handlers, a reporting job, ...) depend on this, never on the command side.
// Splitting reads out like this is what lets the two sides scale and evolve
// independently.
type Queries interface {
	GetOrder(ctx context.Context, id string) (DetailView, error)
	ListOrders(ctx context.Context, limit, offset int) ([]SummaryView, error)
	ListCustomerOrders(ctx context.Context, customerID string) ([]SummaryView, error)
}

// QueryService implements Queries by reading exclusively from the ReadStore. It
// holds no aggregate and enforces no invariants — it only serves projections.
type QueryService struct {
	read ReadStore
}

// NewQueryService builds the read-side service over a ReadStore.
func NewQueryService(read ReadStore) *QueryService {
	return &QueryService{read: read}
}

var _ Queries = (*QueryService)(nil)

// GetOrder returns a single order detail projection.
func (q *QueryService) GetOrder(ctx context.Context, id string) (DetailView, error) {
	return q.read.Get(ctx, id)
}

// ListOrders returns order summaries with clamped pagination.
func (q *QueryService) ListOrders(ctx context.Context, limit, offset int) ([]SummaryView, error) {
	limit, offset = clampPage(limit, offset)
	return q.read.List(ctx, limit, offset)
}

// ListCustomerOrders returns a customer's order summaries.
func (q *QueryService) ListCustomerOrders(ctx context.Context, customerID string) ([]SummaryView, error) {
	return q.read.ListByCustomer(ctx, customerID)
}

func clampPage(limit, offset int) (int, int) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
