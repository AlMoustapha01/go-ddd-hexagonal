package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/example/go-ddd-hex/internal/domain/user"
)

// UserRepository implements domain.Repository against PostgreSQL. It is the
// only place that knows the SQL schema; it maps rows <-> aggregate via
// domain.Rehydrate so the domain stays persistence-ignorant.
type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

var _ domain.Repository = (*UserRepository)(nil)

func (r *UserRepository) Save(ctx context.Context, u *domain.User) error {
	const q = `
		INSERT INTO users (id, name, email, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			email = EXCLUDED.email,
			active = EXCLUDED.active,
			updated_at = EXCLUDED.updated_at`
	_, err := r.pool.Exec(ctx, q,
		u.ID().String(), u.Name(), u.Email().String(), u.IsActive(),
		u.CreatedAt(), u.UpdatedAt())
	if err != nil {
		return fmt.Errorf("postgres: save user: %w", err)
	}
	return nil
}

func (r *UserRepository) FindByID(ctx context.Context, id domain.ID) (*domain.User, error) {
	const q = `SELECT id, name, email, active, created_at, updated_at FROM users WHERE id = $1`
	return r.scanOne(ctx, r.pool.QueryRow(ctx, q, id.String()))
}

func (r *UserRepository) FindByEmail(ctx context.Context, email domain.Email) (*domain.User, error) {
	const q = `SELECT id, name, email, active, created_at, updated_at FROM users WHERE email = $1`
	return r.scanOne(ctx, r.pool.QueryRow(ctx, q, email.String()))
}

func (r *UserRepository) ExistsByEmail(ctx context.Context, email domain.Email) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)`
	var exists bool
	if err := r.pool.QueryRow(ctx, q, email.String()).Scan(&exists); err != nil {
		return false, fmt.Errorf("postgres: exists by email: %w", err)
	}
	return exists, nil
}

func (r *UserRepository) List(ctx context.Context, limit, offset int) ([]*domain.User, error) {
	const q = `
		SELECT id, name, email, active, created_at, updated_at
		FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list users: %w", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		u, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate users: %w", err)
	}
	return users, nil
}

func (r *UserRepository) Delete(ctx context.Context, id domain.ID) error {
	const q = `DELETE FROM users WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id.String())
	if err != nil {
		return fmt.Errorf("postgres: delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// row is the minimal interface satisfied by both pgx.Row and pgx.Rows.
type row interface {
	Scan(dest ...any) error
}

func (r *UserRepository) scanOne(_ context.Context, rw pgx.Row) (*domain.User, error) {
	u, err := scanRow(rw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return u, err
}

func scanRow(rw row) (*domain.User, error) {
	var (
		id, name, emailRaw string
		active             bool
		createdAt          time.Time
		updatedAt          time.Time
	)
	if err := rw.Scan(&id, &name, &emailRaw, &active, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("postgres: scan user: %w", err)
	}

	uid, err := domain.IDFromString(id)
	if err != nil {
		return nil, fmt.Errorf("postgres: corrupt id %q: %w", id, err)
	}
	email, err := domain.NewEmail(emailRaw)
	if err != nil {
		return nil, fmt.Errorf("postgres: corrupt email %q: %w", emailRaw, err)
	}
	return domain.Rehydrate(uid, name, email, active, createdAt, updatedAt), nil
}
