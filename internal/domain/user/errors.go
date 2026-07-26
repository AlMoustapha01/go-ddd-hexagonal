package user

import "errors"

// Domain errors. The application and adapter layers inspect these with
// errors.Is to translate them into transport-specific responses (HTTP codes,
// etc.) without leaking domain internals.
var (
	ErrEmailEmpty      = errors.New("user: email must not be empty")
	ErrEmailInvalid    = errors.New("user: email format is invalid")
	ErrNameEmpty       = errors.New("user: name must not be empty")
	ErrNotFound        = errors.New("user: not found")
	ErrEmailTaken      = errors.New("user: email already registered")
	ErrAlreadyActive   = errors.New("user: account already active")
	ErrAlreadyInactive = errors.New("user: account already inactive")
)
