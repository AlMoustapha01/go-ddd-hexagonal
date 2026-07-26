package order

import "fmt"

// Money is a value object representing an amount in the smallest currency unit
// (e.g. cents). Storing minor units as an integer avoids floating-point errors.
// Money is immutable: arithmetic returns new values.
type Money struct {
	amount   int64  // minor units, e.g. 1050 == 10.50
	currency string // ISO-4217, e.g. "EUR", "XOF"
}

// NewMoney builds a Money value object, validating the currency and sign.
func NewMoney(amount int64, currency string) (Money, error) {
	if len(currency) != 3 {
		return Money{}, ErrInvalidCurrency
	}
	if amount < 0 {
		return Money{}, ErrNegativeAmount
	}
	return Money{amount: amount, currency: currency}, nil
}

// Zero returns a zero amount in the given currency.
func Zero(currency string) Money { return Money{amount: 0, currency: currency} }

func (m Money) Amount() int64    { return m.amount }
func (m Money) Currency() string { return m.currency }
func (m Money) IsZero() bool     { return m.amount == 0 }

// Add returns the sum, enforcing that both operands share a currency.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, ErrCurrencyMismatch
	}
	return Money{amount: m.amount + other.amount, currency: m.currency}, nil
}

// MultiplyInt scales the amount by a non-negative integer factor.
func (m Money) MultiplyInt(factor int) Money {
	return Money{amount: m.amount * int64(factor), currency: m.currency}
}

// Equals compares two Money value objects by value.
func (m Money) Equals(other Money) bool {
	return m.amount == other.amount && m.currency == other.currency
}

// String renders the amount with two decimals (sufficient for the example).
func (m Money) String() string {
	return fmt.Sprintf("%d.%02d %s", m.amount/100, m.amount%100, m.currency)
}
