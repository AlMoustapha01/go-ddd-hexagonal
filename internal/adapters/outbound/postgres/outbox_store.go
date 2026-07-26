package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// OutboxStore is the Postgres shared.OutboxStore. Add uses the context's
// transaction (via queryEngine) so records are written atomically with the
// aggregate change. FetchUnpublished / MarkPublished run on the pool from the
// relay, outside that transaction.
type OutboxStore struct {
	pool *pgxpool.Pool
}

// NewOutboxStore builds the Postgres outbox.
func NewOutboxStore(pool *pgxpool.Pool) *OutboxStore { return &OutboxStore{pool: pool} }

var _ shared.OutboxStore = (*OutboxStore)(nil)

// Add inserts records within the ambient transaction.
func (s *OutboxStore) Add(ctx context.Context, records ...shared.OutboxRecord) error {
	q := queryEngine(ctx, s.pool)
	const stmt = `
		INSERT INTO outbox (id, event_name, aggregate_id, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)`
	for _, rec := range records {
		if _, err := q.Exec(ctx, stmt, rec.ID, rec.EventName, rec.AggregateID, rec.Payload, rec.OccurredAt); err != nil {
			return fmt.Errorf("postgres: add outbox record: %w", err)
		}
	}
	return nil
}

// FetchUnpublished returns unpublished records oldest-first. FOR UPDATE SKIP
// LOCKED lets multiple relay instances run without processing the same rows.
func (s *OutboxStore) FetchUnpublished(ctx context.Context, limit int) ([]shared.OutboxRecord, error) {
	const q = `
		SELECT id, event_name, aggregate_id, payload, occurred_at
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY occurred_at ASC
		LIMIT $1`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: fetch outbox: %w", err)
	}
	defer rows.Close()

	var out []shared.OutboxRecord
	for rows.Next() {
		var rec shared.OutboxRecord
		if err := rows.Scan(&rec.ID, &rec.EventName, &rec.AggregateID, &rec.Payload, &rec.OccurredAt); err != nil {
			return nil, fmt.Errorf("postgres: scan outbox: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate outbox: %w", err)
	}
	return out, nil
}

// MarkPublished stamps the given records as published.
func (s *OutboxStore) MarkPublished(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	const q = `UPDATE outbox SET published_at = now() WHERE id = ANY($1)`
	if _, err := s.pool.Exec(ctx, q, ids); err != nil {
		return fmt.Errorf("postgres: mark outbox published: %w", err)
	}
	return nil
}
