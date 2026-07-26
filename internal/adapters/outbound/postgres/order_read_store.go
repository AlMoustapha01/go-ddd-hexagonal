package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apporder "github.com/example/go-ddd-hex/internal/application/order"
	domainorder "github.com/example/go-ddd-hex/internal/domain/order"
)

// OrderReadStore is the PostgreSQL CQRS read model. It uses its OWN table
// (order_read), denormalized for querying, distinct from the write-side
// "orders" table. In a bigger system this could even live in a different
// database or engine; the port makes that swap invisible to the query service.
type OrderReadStore struct {
	pool *pgxpool.Pool
}

// NewOrderReadStore builds the Postgres read store.
func NewOrderReadStore(pool *pgxpool.Pool) *OrderReadStore {
	return &OrderReadStore{pool: pool}
}

var _ apporder.ReadStore = (*OrderReadStore)(nil)

// Upsert inserts or replaces a projection row.
func (s *OrderReadStore) Upsert(ctx context.Context, d apporder.DetailView) error {
	linesJSON, err := json.Marshal(d.Lines)
	if err != nil {
		return fmt.Errorf("postgres: marshal read lines: %w", err)
	}
	const q = `
		INSERT INTO order_read
			(id, customer_id, status, currency, total_minor, item_count, lines, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			total_minor = EXCLUDED.total_minor,
			item_count = EXCLUDED.item_count,
			lines = EXCLUDED.lines,
			updated_at = EXCLUDED.updated_at`
	_, err = s.pool.Exec(ctx, q,
		d.ID, d.CustomerID, d.Status, d.Currency, d.TotalMinor, d.ItemCount,
		linesJSON, d.CreatedAt, d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: upsert read order: %w", err)
	}
	return nil
}

// Delete removes a projection row.
func (s *OrderReadStore) Delete(ctx context.Context, id string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM order_read WHERE id = $1`, id); err != nil {
		return fmt.Errorf("postgres: delete read order: %w", err)
	}
	return nil
}

// Get returns a detail projection or domain order.ErrNotFound.
func (s *OrderReadStore) Get(ctx context.Context, id string) (apporder.DetailView, error) {
	const q = `
		SELECT id, customer_id, status, currency, total_minor, item_count, lines, created_at, updated_at
		FROM order_read WHERE id = $1`
	var (
		d        apporder.DetailView
		linesRaw []byte
	)
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&d.ID, &d.CustomerID, &d.Status, &d.Currency, &d.TotalMinor, &d.ItemCount,
		&linesRaw, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apporder.DetailView{}, domainorder.ErrNotFound
	}
	if err != nil {
		return apporder.DetailView{}, fmt.Errorf("postgres: get read order: %w", err)
	}
	if len(linesRaw) > 0 {
		if err := json.Unmarshal(linesRaw, &d.Lines); err != nil {
			return apporder.DetailView{}, fmt.Errorf("postgres: unmarshal read lines: %w", err)
		}
	}
	return d, nil
}

// List returns summaries, newest first, paginated.
func (s *OrderReadStore) List(ctx context.Context, limit, offset int) ([]apporder.SummaryView, error) {
	const q = `
		SELECT id, customer_id, status, currency, total_minor, item_count, updated_at
		FROM order_read ORDER BY updated_at DESC LIMIT $1 OFFSET $2`
	return s.querySummaries(ctx, q, limit, offset)
}

// ListByCustomer returns a customer's summaries, newest first.
func (s *OrderReadStore) ListByCustomer(ctx context.Context, customerID string) ([]apporder.SummaryView, error) {
	const q = `
		SELECT id, customer_id, status, currency, total_minor, item_count, updated_at
		FROM order_read WHERE customer_id = $1 ORDER BY updated_at DESC`
	return s.querySummaries(ctx, q, customerID)
}

func (s *OrderReadStore) querySummaries(ctx context.Context, q string, args ...any) ([]apporder.SummaryView, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: query read summaries: %w", err)
	}
	defer rows.Close()

	var out []apporder.SummaryView
	for rows.Next() {
		var v apporder.SummaryView
		if err := rows.Scan(&v.ID, &v.CustomerID, &v.Status, &v.Currency, &v.TotalMinor, &v.ItemCount, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan read summary: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate read summaries: %w", err)
	}
	return out, nil
}
