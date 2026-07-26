package account_test

import (
	"testing"

	"github.com/example/go-ddd-hex/internal/domain/account"
	"github.com/example/go-ddd-hex/internal/domain/shared"
)

func TestFoldProducesBalance(t *testing.T) {
	a, err := account.Open("EUR")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := a.Deposit(1000); err != nil {
		t.Fatalf("deposit: %v", err)
	}
	if err := a.Withdraw(400); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if a.Balance() != 600 {
		t.Fatalf("balance: got %d, want 600", a.Balance())
	}
	// Three events pending (Opened, Deposited, Withdrawn), version still 0
	// because nothing has been persisted yet.
	if a.Version() != 0 {
		t.Fatalf("version before persist: got %d, want 0", a.Version())
	}
	if got := len(a.PendingEvents()); got != 3 {
		t.Fatalf("pending events: got %d, want 3", got)
	}
}

func TestInvariants(t *testing.T) {
	a, _ := account.Open("EUR")
	_ = a.Deposit(500)

	if err := a.Withdraw(0); err != account.ErrInvalidAmount {
		t.Errorf("withdraw 0: got %v, want ErrInvalidAmount", err)
	}
	if err := a.Withdraw(9999); err != account.ErrInsufficientFunds {
		t.Errorf("overdraw: got %v, want ErrInsufficientFunds", err)
	}
	if err := a.Deposit(-5); err != account.ErrInvalidAmount {
		t.Errorf("negative deposit: got %v, want ErrInvalidAmount", err)
	}
}

// TestReplayEqualsLive is the defining event-sourcing property: an aggregate
// rebuilt from its persisted event stream is identical to the live one.
func TestReplayEqualsLive(t *testing.T) {
	live, _ := account.Open("USD")
	_ = live.Deposit(2500)
	_ = live.Withdraw(1000)
	_ = live.Deposit(750)

	stream := live.PendingEvents() // the events that would be persisted

	replayed := account.Rehydrate(stream)
	if replayed.Balance() != live.Balance() {
		t.Fatalf("replayed balance %d != live balance %d", replayed.Balance(), live.Balance())
	}
	if replayed.Currency() != "USD" {
		t.Fatalf("replayed currency: got %q, want USD", replayed.Currency())
	}
	// The rehydrated aggregate's version equals the number of persisted events.
	if replayed.Version() != len(stream) {
		t.Fatalf("replayed version: got %d, want %d", replayed.Version(), len(stream))
	}
}

func TestCodecRoundTrip(t *testing.T) {
	a, _ := account.Open("XOF")
	_ = a.Deposit(12345)
	_ = a.Withdraw(45)
	original := a.PendingEvents()

	// Encode then decode each event; the decoded stream must fold to the same state.
	decoded := make([]shared.Event, 0, len(original))
	for _, e := range original {
		name, payload, err := account.Encode(e)
		if err != nil {
			t.Fatalf("encode %s: %v", e.EventName(), err)
		}
		back, err := account.Decode(name, payload)
		if err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		if back.EventName() != e.EventName() {
			t.Fatalf("decoded name %q != %q", back.EventName(), e.EventName())
		}
		decoded = append(decoded, back)
	}
	if got := account.Rehydrate(decoded).Balance(); got != 12300 {
		t.Fatalf("balance after codec round-trip: got %d, want 12300", got)
	}
}
