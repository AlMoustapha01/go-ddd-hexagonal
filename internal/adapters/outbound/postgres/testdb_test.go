package postgres_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	pg "github.com/example/go-ddd-hex/internal/adapters/outbound/postgres"
)

// These tests need a real PostgreSQL instance, because the whole point is to
// exercise the SQL that `go build` cannot check. They are skipped — not failed —
// when TEST_DATABASE_URL is unset, so `go test ./...` stays green on a machine
// without a database. `make test-integration` provides one.
//
// No build tag guards this file on purpose: a tagged file is invisible to the
// compiler and the linter in the normal job, so it can rot silently. That is
// exactly the failure mode these tests exist to remove.

var (
	setupOnce  sync.Once
	sharedPool *pgxpool.Pool
	setupErr   error
)

// freshDB returns a pool on the test database with the schema applied and every
// table emptied. Callers must not run in parallel: the database is shared, and
// isolation comes from truncating between sub-tests.
func freshDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the Postgres contract (run `make test-integration`)")
	}

	setupOnce.Do(func() { sharedPool, setupErr = openAndMigrate(dsn) })
	if setupErr != nil {
		t.Fatalf("prepare test database: %v", setupErr)
	}

	truncateAll(t, sharedPool)
	return sharedPool
}

// openAndMigrate connects and applies the repository's migrations. Every CREATE
// in them is already IF NOT EXISTS, so this is idempotent and coexists happily
// with a database already migrated by `make docker-up`.
func openAndMigrate(dsn string) (*pgxpool.Pool, error) {
	ctx := context.Background()

	pool, err := pg.NewPool(ctx, dsn)
	if err != nil {
		return nil, err
	}

	files, err := migrationFiles()
	if err != nil {
		pool.Close()
		return nil, err
	}
	for _, f := range files {
		stmt, err := os.ReadFile(f) //nolint:gosec // test-only, path derived from the repo layout
		if err != nil {
			pool.Close()
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		if _, err := pool.Exec(ctx, string(stmt)); err != nil {
			pool.Close()
			return nil, fmt.Errorf("apply %s: %w", filepath.Base(f), err)
		}
	}
	return pool, nil
}

// migrationFiles returns the *.up.sql files in lexical (therefore numeric) order.
func migrationFiles() ([]string, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(root, "migrations", "*.up.sql"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no migrations found under %s", filepath.Join(root, "migrations"))
	}
	sort.Strings(files)
	return files, nil
}

// repoRoot walks up from the test's working directory to the directory holding
// go.mod, so the migrations are found wherever `go test` is invoked from.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above the working directory")
		}
		dir = parent
	}
}

// truncateAll empties every table so each sub-test starts from a known state.
func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	const stmt = `TRUNCATE users, orders, order_read, outbox, account_events RESTART IDENTITY CASCADE`
	if _, err := pool.Exec(context.Background(), stmt); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}
