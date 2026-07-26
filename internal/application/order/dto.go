package order

import (
	"time"

	domain "github.com/example/go-ddd-hex/internal/domain/order"
)

type CreateCommand struct {
	CustomerID string
	Currency   string
}

type AddLineCommand struct {
	OrderID        string
	ProductID      string
	UnitPriceMinor int64
	Currency       string
	Quantity       int
}

// LineView is a flat projection of an order line.
type LineView struct {
	ProductID string `json:"product_id"`
	UnitPrice int64  `json:"unit_price_minor"`
	Currency  string `json:"currency"`
	Quantity  int    `json:"quantity"`
	Subtotal  int64  `json:"subtotal_minor"`
}

// OrderView is the read model returned by the order use cases.
type OrderView struct {
	ID         string     `json:"id"`
	CustomerID string     `json:"customer_id"`
	Currency   string     `json:"currency"`
	Status     string     `json:"status"`
	Lines      []LineView `json:"lines"`
	TotalMinor int64      `json:"total_minor"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func toView(o *domain.Order) OrderView {
	lines := make([]LineView, 0, len(o.Lines()))
	for _, l := range o.Lines() {
		lines = append(lines, LineView{
			ProductID: l.ProductID(),
			UnitPrice: l.UnitPrice().Amount(),
			Currency:  l.UnitPrice().Currency(),
			Quantity:  l.Quantity(),
			Subtotal:  l.Subtotal().Amount(),
		})
	}
	total, _ := o.Total()
	return OrderView{
		ID:         o.ID().String(),
		CustomerID: o.CustomerID().String(),
		Currency:   o.Currency(),
		Status:     string(o.Status()),
		Lines:      lines,
		TotalMinor: total.Amount(),
		CreatedAt:  o.CreatedAt(),
		UpdatedAt:  o.UpdatedAt(),
	}
}
