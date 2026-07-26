package user

import (
	"regexp"
	"strings"
)

// emailRegex is a pragmatic (not RFC-5322-complete) email validator.
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// Email is a value object. It is immutable and always valid once constructed.
// Value objects have no identity: two Emails with the same address are equal.
type Email struct {
	value string
}

// NewEmail validates and normalizes an email address, returning a value object.
func NewEmail(raw string) (Email, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return Email{}, ErrEmailEmpty
	}
	if !emailRegex.MatchString(normalized) {
		return Email{}, ErrEmailInvalid
	}
	return Email{value: normalized}, nil
}

// String returns the underlying address.
func (e Email) String() string { return e.value }

// Equals compares two Email value objects by value.
func (e Email) Equals(other Email) bool { return e.value == other.value }

// IsZero reports whether the Email is the zero value (never constructed).
func (e Email) IsZero() bool { return e.value == "" }
