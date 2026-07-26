package order_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	app "github.com/example/go-ddd-hex/internal/application/order"
	domain "github.com/example/go-ddd-hex/internal/domain/order"
)

// recordingReadStore captures the pagination the query service actually passed
// down, which is the only way to observe the clamping.
type recordingReadStore struct {
	app.ReadStore
	limit, offset int
}

func (s *recordingReadStore) List(ctx context.Context, limit, offset int) ([]app.SummaryView, error) {
	s.limit, s.offset = limit, offset
	return s.ReadStore.List(ctx, limit, offset)
}

func TestListOrdersClampsPagination(t *testing.T) {
	// The clamping lives in the query service, not the handler, so that every
	// driving adapter — HTTP today, a CLI or gRPC tomorrow — inherits the same
	// bound. A caller must not be able to ask for an unbounded page.
	ctx := context.Background()

	cases := []struct {
		name                  string
		limit, offset         int
		wantLimit, wantOffset int
	}{
		{"a zero limit falls back to the default", 0, 0, 20, 0},
		{"a negative limit falls back to the default", -1, 0, 20, 0},
		{"an excessive limit falls back to the default", 101, 0, 20, 0},
		{"the maximum is allowed through", 100, 0, 100, 0},
		{"a negative offset is clamped to zero", 10, -7, 10, 0},
		{"a sane page is passed through", 25, 50, 25, 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingReadStore{ReadStore: memory.NewOrderReadStore()}
			q := app.NewQueryService(store)

			if _, err := q.ListOrders(ctx, tc.limit, tc.offset); err != nil {
				t.Fatalf("ListOrders: %v", err)
			}
			if store.limit != tc.wantLimit {
				t.Errorf("limit: got %d, want %d", store.limit, tc.wantLimit)
			}
			if store.offset != tc.wantOffset {
				t.Errorf("offset: got %d, want %d", store.offset, tc.wantOffset)
			}
		})
	}
}

func TestQueriesReadFromTheReadStore(t *testing.T) {
	// The query side must never touch the write repository — that separation is
	// the point of CQRS. Here the read store is seeded directly, with no write
	// model behind it at all, and the queries still answer.
	ctx := context.Background()
	store := memory.NewOrderReadStore()
	q := app.NewQueryService(store)

	orderID := domain.NewID().String()
	customerID := newCustomerID()
	if err := store.Upsert(ctx, app.DetailView{
		SummaryView: app.SummaryView{
			ID: orderID, CustomerID: customerID, Status: "placed", Currency: "EUR",
			TotalMinor: 2100, ItemCount: 1,
		},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := q.GetOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if got.TotalMinor != 2100 {
		t.Errorf("total: got %d, want %d", got.TotalMinor, 2100)
	}

	summaries, err := q.ListCustomerOrders(ctx, customerID)
	if err != nil {
		t.Fatalf("ListCustomerOrders: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != orderID {
		t.Errorf("customer orders: got %+v", summaries)
	}

	if _, err := q.GetOrder(ctx, domain.NewID().String()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown order: got %v, want ErrNotFound", err)
	}

	empty, err := q.ListCustomerOrders(ctx, newCustomerID())
	if err != nil {
		t.Fatalf("ListCustomerOrders for an unknown customer: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d summaries, want 0", len(empty))
	}
}
