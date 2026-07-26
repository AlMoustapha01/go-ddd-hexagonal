package user_test

import (
	"testing"

	"github.com/example/go-ddd-hex/internal/domain/user"
)

func mustEmail(t *testing.T, raw string) user.Email {
	t.Helper()
	e, err := user.NewEmail(raw)
	if err != nil {
		t.Fatalf("NewEmail(%q) unexpected error: %v", raw, err)
	}
	return e
}

func TestNewEmail(t *testing.T) {
	t.Run("normalizes and accepts a valid address", func(t *testing.T) {
		e, err := user.NewEmail("  Alice@Example.COM ")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if e.String() != "alice@example.com" {
			t.Fatalf("got %q, want normalized alice@example.com", e.String())
		}
	})

	t.Run("rejects empty and malformed addresses", func(t *testing.T) {
		for _, raw := range []string{"", "   ", "not-an-email", "a@b", "@example.com"} {
			if _, err := user.NewEmail(raw); err == nil {
				t.Errorf("NewEmail(%q) expected error, got nil", raw)
			}
		}
	})
}

func TestRegister(t *testing.T) {
	t.Run("creates an inactive user and records an event", func(t *testing.T) {
		u, err := user.Register("Alice", mustEmail(t, "alice@example.com"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.IsActive() {
			t.Error("newly registered user should be inactive")
		}
		if u.ID().IsZero() {
			t.Error("user should have an identity")
		}
		events := u.PullEvents()
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].EventName() != "user.registered" {
			t.Errorf("unexpected event name: %s", events[0].EventName())
		}
		// Events are cleared after pulling.
		if len(u.PullEvents()) != 0 {
			t.Error("events should be cleared after PullEvents")
		}
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		if _, err := user.Register("   ", mustEmail(t, "a@example.com")); err != user.ErrNameEmpty {
			t.Errorf("got %v, want ErrNameEmpty", err)
		}
	})
}

func TestActivationInvariants(t *testing.T) {
	u, _ := user.Register("Bob", mustEmail(t, "bob@example.com"))
	u.PullEvents()

	if err := u.Activate(); err != nil {
		t.Fatalf("first Activate should succeed: %v", err)
	}
	if !u.IsActive() {
		t.Fatal("user should be active")
	}
	if err := u.Activate(); err != user.ErrAlreadyActive {
		t.Errorf("second Activate: got %v, want ErrAlreadyActive", err)
	}
	if err := u.Deactivate(); err != nil {
		t.Fatalf("Deactivate should succeed: %v", err)
	}
	if err := u.Deactivate(); err != user.ErrAlreadyInactive {
		t.Errorf("second Deactivate: got %v, want ErrAlreadyInactive", err)
	}
}
