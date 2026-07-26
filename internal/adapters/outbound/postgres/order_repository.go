package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	order "github.com/example/go-ddd-hex/internal/domain/order"
)

// OrderRepository persists Order aggregates in PostgreSQL. The lines (part of
// the aggregate) are stored as a JSONB column so the whole aggregate is saved
// and loaded atomically as one unit — a common pattern for aggregates whose
// children are only ever accessed through the root.
type OrderRepository struct {
	pool *pgxpool.Pool
}

func NewOrderRepository(pool *pgxpool.Pool) *OrderRepository {
	return &OrderRepository{pool: pool}
}

var _ order.Repository = (*OrderRepository)(nil)

// lineDTO is the on-disk representation of an order line (JSONB element).
type lineDTO struct {
	ProductID string `json:"product_id"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Quantity  int    `json:"quantity"`
}

func (r *OrderRepository) Save(ctx context.Context, o *order.Order) error {
	linesJSON, err := marshalLines(o)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO orders (id, customer_id, currency, status, lines, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			lines = EXCLUDED.lines,
			updated_at = EXCLUDED.updated_at`
	// queryEngine picks up the Unit of Work transaction from the context so the
	// order row and its outbox records commit atomically.
	_, err = queryEngine(ctx, r.pool).Exec(ctx, q,
		o.ID().String(), o.CustomerID().String(), o.Currency(), string(o.Status()),
		linesJSON, o.CreatedAt(), o.UpdatedAt())
	if err != nil {
		return fmt.Errorf("postgres: save order: %w", err)
	}
	return nil
}

func (r *OrderRepository) FindByID(ctx context.Context, id order.ID) (*order.Order, error) {
	const q = `SELECT id, customer_id, currency, status, lines, created_at, updated_at FROM orders WHERE id = $1`
	o, err := scanOrder(r.pool.QueryRow(ctx, q, id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, order.ErrNotFound
	}
	return o, err
}

func (r *OrderRepository) ListByCustomer(ctx context.Context, customerID order.CustomerID) ([]*order.Order, error) {
	const q = `
		SELECT id, customer_id, currency, status, lines, created_at, updated_at
		FROM orders WHERE customer_id = $1 ORDER BY created_at DESC`
	return r.queryOrders(ctx, q, customerID.String())
}

func (r *OrderRepository) List(ctx context.Context, limit, offset int) ([]*order.Order, error) {
	const q = `
		SELECT id, customer_id, currency, status, lines, created_at, updated_at
		FROM orders ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	return r.queryOrders(ctx, q, limit, offset)
}

func (r *OrderRepository) Delete(ctx context.Context, id order.ID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("postgres: delete order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return order.ErrNotFound
	}
	return nil
}

func (r *OrderRepository) queryOrders(ctx context.Context, q string, args ...any) ([]*order.Order, error) {
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: query orders: %w", err)
	}
	defer rows.Close()

	// Non-nil so an empty result serialises as [] and not null.
	orders := make([]*order.Order, 0)
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate orders: %w", err)
	}
	return orders, nil
}

func marshalLines(o *order.Order) ([]byte, error) {
	dtos := make([]lineDTO, 0, len(o.Lines()))
	for _, l := range o.Lines() {
		dtos = append(dtos, lineDTO{
			ProductID: l.ProductID(),
			Amount:    l.UnitPrice().Amount(),
			Currency:  l.UnitPrice().Currency(),
			Quantity:  l.Quantity(),
		})
	}
	b, err := json.Marshal(dtos)
	if err != nil {
		return nil, fmt.Errorf("postgres: marshal lines: %w", err)
	}
	return b, nil
}

func scanOrder(rw pgx.Row) (*order.Order, error) {
	var (
		id, customerID, currency, status string
		linesRaw                         []byte
		createdAt, updatedAt             time.Time
	)
	if err := rw.Scan(&id, &customerID, &currency, &status, &linesRaw, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("postgres: scan order: %w", err)
	}

	oid, err := order.IDFromString(id)
	if err != nil {
		return nil, fmt.Errorf("postgres: corrupt order id %q: %w", id, err)
	}
	cid, err := order.CustomerIDFromString(customerID)
	if err != nil {
		return nil, fmt.Errorf("postgres: corrupt customer id %q: %w", customerID, err)
	}

	var dtos []lineDTO
	if len(linesRaw) > 0 {
		if err := json.Unmarshal(linesRaw, &dtos); err != nil {
			return nil, fmt.Errorf("postgres: unmarshal lines: %w", err)
		}
	}
	lines := make([]order.Line, 0, len(dtos))
	for _, d := range dtos {
		price, err := order.NewMoney(d.Amount, d.Currency)
		if err != nil {
			return nil, fmt.Errorf("postgres: corrupt line price: %w", err)
		}
		line, err := order.NewLine(d.ProductID, price, d.Quantity)
		if err != nil {
			return nil, fmt.Errorf("postgres: corrupt line: %w", err)
		}
		lines = append(lines, line)
	}

	return order.Rehydrate(oid, cid, currency, order.Status(status), lines, createdAt, updatedAt), nil
}
