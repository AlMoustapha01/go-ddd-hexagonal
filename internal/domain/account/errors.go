package account

import "errors"

var (
	ErrNotFound          = errors.New("account: not found")
	ErrInvalidCurrency   = errors.New("account: currency must be a 3-letter code")
	ErrInvalidAmount     = errors.New("account: amount must be positive")
	ErrInsufficientFunds = errors.New("account: insufficient funds")
	// ErrConcurrencyConflict is returned by the event store when the expected
	// version does not match the stored one (optimistic concurrency).
	ErrConcurrencyConflict = errors.New("account: concurrency conflict")
	// ErrUnknownEvent is returned by the codec for an unregistered event name.
	ErrUnknownEvent = errors.New("account: unknown event type")
)
