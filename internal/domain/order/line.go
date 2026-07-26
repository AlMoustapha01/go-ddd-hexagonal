package order

import "strings"

// Line is a value object: one product line within an Order. It has no identity
// of its own — it only exists as part of its Order aggregate, and is created
// through the aggregate so invariants are enforced in one place.
type Line struct {
	productID string
	unitPrice Money
	quantity  int
}

// NewLine validates and builds an order line.
func NewLine(productID string, unitPrice Money, quantity int) (Line, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return Line{}, ErrEmptyProduct
	}
	if quantity <= 0 {
		return Line{}, ErrInvalidQuantity
	}
	return Line{productID: productID, unitPrice: unitPrice, quantity: quantity}, nil
}

func (l Line) ProductID() string { return l.productID }
func (l Line) UnitPrice() Money  { return l.unitPrice }
func (l Line) Quantity() int     { return l.quantity }

// Subtotal is unitPrice * quantity.
func (l Line) Subtotal() Money { return l.unitPrice.MultiplyInt(l.quantity) }
