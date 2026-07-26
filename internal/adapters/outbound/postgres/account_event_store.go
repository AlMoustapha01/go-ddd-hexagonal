package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	account "github.com/example/go-ddd-hex/internal/domain/account"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// AccountEventStore persists the event-sourced Account in an append-only table.
// Optimistic concurrency is enforced by the PRIMARY KEY (aggregate_id, version):
// two writers that both try to append the same next version cannot both succeed
// — the second gets a unique-violation, surfaced as ErrConcurrencyConflict.
type AccountEventStore struct {
	pool *pgxpool.Pool
}

// NewAccountEventStore builds the Postgres event store.
func NewAccountEventStore(pool *pgxpool.Pool) *AccountEventStore {
	return &AccountEventStore{pool: pool}
}

var _ account.EventStore = (*AccountEventStore)(nil)

// Load reads and decodes the ordered stream.
func (s *AccountEventStore) Load(ctx context.Context, id account.ID) ([]shared.Event, error) {
	const q = `
		SELECT event_name, payload
		FROM account_events
		WHERE aggregate_id = $1
		ORDER BY version ASC`
	rows, err := s.pool.Query(ctx, q, id.String())
	if err != nil {
		return nil, fmt.Errorf("postgres: load account events: %w", err)
	}
	defer rows.Close()

	var events []shared.Event
	for rows.Next() {
		var (
			name    string
			payload []byte
		)
		if err := rows.Scan(&name, &payload); err != nil {
			return nil, fmt.Errorf("postgres: scan account event: %w", err)
		}
		e, err := account.Decode(name, payload)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate account events: %w", err)
	}
	if len(events) == 0 {
		return nil, account.ErrNotFound
	}
	return events, nil
}

// Append writes the events with sequential versions in one transaction. A
// unique-violation on (aggregate_id, version) means another writer won the race.
func (s *AccountEventStore) Append(ctx context.Context, id account.ID, expectedVersion int, events []shared.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const stmt = `
		INSERT INTO account_events (aggregate_id, version, event_name, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)`
	for i, e := range events {
		name, payload, err := account.Encode(e)
		if err != nil {
			return err
		}
		version := expectedVersion + i + 1
		if _, err := tx.Exec(ctx, stmt, id.String(), version, name, payload, e.OccurredAt()); err != nil {
			if isUniqueViolation(err) {
				return account.ErrConcurrencyConflict
			}
			return fmt.Errorf("postgres: append account event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit append: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
