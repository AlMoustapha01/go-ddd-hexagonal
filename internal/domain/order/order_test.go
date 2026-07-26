package order_test

import (
	"testing"

	"github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/google/uuid"
)

func customer(t *testing.T) order.CustomerID {
	t.Helper()
	c, err := order.CustomerIDFromString(uuid.NewString())
	if err != nil {
		t.Fatalf("customer id: %v", err)
	}
	return c
}

func money(t *testing.T, amount int64, cur string) order.Money {
	t.Helper()
	m, err := order.NewMoney(amount, cur)
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	return m
}

func TestPlaceComputesTotalAndEmitsEvent(t *testing.T) {
	o, err := order.NewDraft(customer(t), "EUR")
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if err := o.AddLine("SKU-1", money(t, 1000, "EUR"), 2); err != nil {
		t.Fatalf("add line 1: %v", err)
	}
	if err := o.AddLine("SKU-2", money(t, 550, "EUR"), 1); err != nil {
		t.Fatalf("add line 2: %v", err)
	}

	if err := o.Place(); err != nil {
		t.Fatalf("place: %v", err)
	}
	if o.Status() != order.StatusPlaced {
		t.Fatalf("status: got %s, want placed", o.Status())
	}
	total, _ := o.Total()
	if total.Amount() != 2550 { // 1000*2 + 550
		t.Errorf("total: got %d, want 2550", total.Amount())
	}

	// Every state change records an event: created, two line_added, placed.
	events := o.PullEvents()
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = e.EventName()
	}
	want := []string{"order.created", "order.line_added", "order.line_added", "order.placed"}
	if len(names) != len(want) {
		t.Fatalf("event sequence: got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("event[%d]: got %q, want %q (full: %v)", i, names[i], want[i], names)
		}
	}
	// Events are cleared after pulling.
	if len(o.PullEvents()) != 0 {
		t.Error("events should be cleared after PullEvents")
	}
}

func TestInvariants(t *testing.T) {
	t.Run("cannot place an empty order", func(t *testing.T) {
		o, _ := order.NewDraft(customer(t), "EUR")
		if err := o.Place(); err != order.ErrNoLines {
			t.Errorf("got %v, want ErrNoLines", err)
		}
	})

	t.Run("cannot add a line in a foreign currency", func(t *testing.T) {
		o, _ := order.NewDraft(customer(t), "EUR")
		if err := o.AddLine("SKU", money(t, 100, "USD"), 1); err != order.ErrLineCurrency {
			t.Errorf("got %v, want ErrLineCurrency", err)
		}
	})

	t.Run("cannot add a line once placed", func(t *testing.T) {
		o, _ := order.NewDraft(customer(t), "EUR")
		_ = o.AddLine("SKU", money(t, 100, "EUR"), 1)
		_ = o.Place()
		if err := o.AddLine("SKU2", money(t, 100, "EUR"), 1); err != order.ErrNotDraft {
			t.Errorf("got %v, want ErrNotDraft", err)
		}
	})

	t.Run("pay requires a placed order", func(t *testing.T) {
		o, _ := order.NewDraft(customer(t), "EUR")
		if err := o.Pay(); err != order.ErrNotPlaced {
			t.Errorf("got %v, want ErrNotPlaced", err)
		}
	})

	t.Run("cannot cancel a paid order", func(t *testing.T) {
		o, _ := order.NewDraft(customer(t), "EUR")
		_ = o.AddLine("SKU", money(t, 100, "EUR"), 1)
		_ = o.Place()
		_ = o.Pay()
		if err := o.Cancel("too late"); err != order.ErrAlreadyFinalized {
			t.Errorf("got %v, want ErrAlreadyFinalized", err)
		}
	})
}

func TestMoneyArithmetic(t *testing.T) {
	a := money(t, 1000, "EUR")
	b := money(t, 250, "EUR")
	sum, err := a.Add(b)
	if err != nil || sum.Amount() != 1250 {
		t.Fatalf("add: got %d err=%v, want 1250", sum.Amount(), err)
	}
	if _, err := a.Add(money(t, 1, "USD")); err != order.ErrCurrencyMismatch {
		t.Errorf("cross-currency add: got %v, want ErrCurrencyMismatch", err)
	}
}
