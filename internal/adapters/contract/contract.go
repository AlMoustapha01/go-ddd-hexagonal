// Package contract holds the behavioural contract of the outbound ports.
//
// Each exported function is a test suite written once against a PORT, and run
// by every adapter that implements it. That is what makes "the in-memory and
// Postgres adapters are interchangeable" a verified property rather than a
// claim: if the two drift apart, the same suite fails on one of them.
//
// This package imports "testing" but is not itself a test package, because both
// adapter test packages must import it. That is the pattern the standard
// library uses for testing/fstest and testing/iotest; nothing outside a test
// imports it, so it never reaches a production binary.
//
// Each suite takes a FACTORY rather than an instance, so that every sub-test
// gets a pristine store and sub-tests cannot leak state into one another.
package contract

import (
	"testing"
	"time"

	order "github.com/example/go-ddd-hex/internal/domain/order"
	user "github.com/example/go-ddd-hex/internal/domain/user"
)

// baseTime anchors every fixture. Timestamps are fixed rather than derived from
// time.Now() for two reasons: PostgreSQL stores TIMESTAMPTZ with microsecond
// precision while Go carries nanoseconds, and rows created in the same
// microsecond would tie under "ORDER BY created_at DESC", making ordering
// assertions flaky. Fixtures are therefore spaced an hour apart.
var baseTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// at returns a fixture timestamp, hours after the anchor.
func at(hours int) time.Time { return baseTime.Add(time.Duration(hours) * time.Hour) }

// mustEmail builds a user.Email or fails the test.
func mustEmail(t *testing.T, raw string) user.Email {
	t.Helper()
	e, err := user.NewEmail(raw)
	if err != nil {
		t.Fatalf("NewEmail(%q): %v", raw, err)
	}
	return e
}

// newUser builds a persisted-shaped user with a controlled creation time.
// Rehydrate is used rather than Register precisely because it lets the test own
// the timestamps.
func newUser(t *testing.T, name, email string, createdAt time.Time) *user.User {
	t.Helper()
	return user.Rehydrate(user.NewID(), name, mustEmail(t, email), false, createdAt, createdAt)
}

// mustCustomerID builds an order.CustomerID or fails the test.
func mustCustomerID(t *testing.T) order.CustomerID {
	t.Helper()
	c, err := order.CustomerIDFromString(user.NewID().String())
	if err != nil {
		t.Fatalf("CustomerIDFromString: %v", err)
	}
	return c
}

// mustMoney builds an order.Money or fails the test.
func mustMoney(t *testing.T, amount int64, currency string) order.Money {
	t.Helper()
	m, err := order.NewMoney(amount, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q): %v", amount, currency, err)
	}
	return m
}

// mustLine builds an order.Line or fails the test.
func mustLine(t *testing.T, productID string, unitPrice order.Money, quantity int) order.Line {
	t.Helper()
	l, err := order.NewLine(productID, unitPrice, quantity)
	if err != nil {
		t.Fatalf("NewLine(%q): %v", productID, err)
	}
	return l
}

// newOrder builds a persisted-shaped order with controlled timestamps.
func newOrder(t *testing.T, customerID order.CustomerID, createdAt time.Time, lines ...order.Line) *order.Order {
	t.Helper()
	return order.Rehydrate(order.NewID(), customerID, "EUR", order.StatusDraft, lines, createdAt, createdAt)
}

// newOrderID and newCustomerID return read-side keys. The read ports take plain
// strings, but the keys really are aggregate ids: the Postgres projection table
// types them as UUID, so fixtures must be shaped like the real thing.
func newOrderID() string    { return order.NewID().String() }
func newCustomerID() string { return user.NewID().String() }
