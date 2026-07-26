package shared_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// aggregate stands in for a real aggregate root: shared.ID needs a phantom type
// and this package must not import a bounded context to borrow one.
type aggregate struct{}

func TestNewIDIsTimeOrdered(t *testing.T) {
	// This is the reason NewID mints a v7 rather than a v4, and the guard
	// against someone quietly reverting it to uuid.New(): identities must sort
	// chronologically so that they append to the right-hand edge of a B-tree
	// primary key instead of scattering across it.
	const n = 10000

	ids := make([]string, n)
	for i := range ids {
		ids[i] = shared.NewID[aggregate]().String()
	}

	t.Run("mints a UUID version 7", func(t *testing.T) {
		u, err := uuid.Parse(ids[0])
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u.Version() != 7 {
			t.Fatalf("got UUID v%d, want v7", u.Version())
		}
	})

	t.Run("generates strictly increasing identities", func(t *testing.T) {
		// Strictly increasing also proves uniqueness: equal neighbours fail here.
		for i := 1; i < len(ids); i++ {
			if ids[i-1] >= ids[i] {
				t.Fatalf("identity %d does not follow its predecessor: %q then %q", i, ids[i-1], ids[i])
			}
		}
	})
}

func TestIDRoundTrip(t *testing.T) {
	original := shared.NewID[aggregate]()

	parsed, err := shared.ParseID[aggregate](original.String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !parsed.Equals(original) {
		t.Errorf("got %s, want %s", parsed, original)
	}
	if parsed.IsZero() {
		t.Error("a parsed identity should not report IsZero")
	}
}

func TestParseIDRejectsMalformedInput(t *testing.T) {
	for _, raw := range []string{"", "   ", "not-a-uuid", "019f9f0f-dc60-747c-91b3"} {
		got, err := shared.ParseID[aggregate](raw)
		if !errors.Is(err, shared.ErrInvalidID) {
			t.Errorf("ParseID(%q): got %v, want ErrInvalidID", raw, err)
		}
		if !got.IsZero() {
			t.Errorf("ParseID(%q): a failed parse should return the zero identity", raw)
		}
	}
}

func TestZeroIdentity(t *testing.T) {
	var zero shared.ID[aggregate]

	if !zero.IsZero() {
		t.Error("the zero value should report IsZero")
	}
	if zero.Equals(shared.NewID[aggregate]()) {
		t.Error("the zero identity should not equal a generated one")
	}
}
