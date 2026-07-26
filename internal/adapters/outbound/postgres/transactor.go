package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// querier is the subset of pgx used by the repositories. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so a repository can run either directly on the pool or
// inside a transaction, transparently.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txKey struct{}

// queryEngine returns the transaction stored on the context if there is one,
// otherwise the pool. This is how repository writes automatically join the
// Unit of Work opened by Transactor.Within.
func queryEngine(ctx context.Context, pool *pgxpool.Pool) querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// Transactor implements shared.Atomic over a pgx pool. Within opens a real
// database transaction, stores it on the context, runs fn, then commits (or
// rolls back on error). Every repository/outbox call made with that context
// participates in the same transaction — that atomicity is what the Outbox
// pattern depends on.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor builds a Postgres unit of work.
func NewTransactor(pool *pgxpool.Pool) *Transactor { return &Transactor{pool: pool} }

var _ shared.Atomic = (*Transactor)(nil)

// Within runs fn inside a single transaction.
func (t *Transactor) Within(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit tx: %w", err)
	}
	return nil
}
