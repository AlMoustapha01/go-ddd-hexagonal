package order

import "errors"

var (
	ErrInvalidCurrency  = errors.New("order: currency must be a 3-letter code")
	ErrNegativeAmount   = errors.New("order: amount must not be negative")
	ErrCurrencyMismatch = errors.New("order: currency mismatch")
	ErrEmptyProduct     = errors.New("order: product id must not be empty")
	ErrInvalidQuantity  = errors.New("order: quantity must be positive")
	ErrNoLines          = errors.New("order: cannot place an order with no lines")
	ErrNotDraft         = errors.New("order: operation only allowed while draft")
	ErrNotPlaced        = errors.New("order: order must be placed first")
	ErrAlreadyFinalized = errors.New("order: order is already paid or cancelled")
	ErrLineCurrency     = errors.New("order: all lines must share the order currency")
	ErrNotFound         = errors.New("order: not found")
	ErrInvalidCustomer  = errors.New("order: invalid customer id")
)
