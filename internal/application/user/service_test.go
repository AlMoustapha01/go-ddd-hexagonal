package user_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	app "github.com/example/go-ddd-hex/internal/application/user"
	"github.com/example/go-ddd-hex/internal/domain/shared"
	domain "github.com/example/go-ddd-hex/internal/domain/user"
)

// The repository collaborator is the real in-memory adapter, not a mock: since
// the contract tests it runs the same suite as the Postgres one, so these tests
// exercise proven behaviour rather than a fiction that can drift. The purpose
// built doubles below exist only to reach branches a correct implementation
// cannot produce — a failing store, a failing bus.

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// recordingPublisher captures what the service dispatched, and can be told to
// fail.
type recordingPublisher struct {
	events []shared.Event
	err    error
}

func (p *recordingPublisher) Publish(_ context.Context, events ...shared.Event) error {
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, events...)
	return nil
}

func (p *recordingPublisher) names() []string {
	out := make([]string, 0, len(p.events))
	for _, e := range p.events {
		out = append(out, e.EventName())
	}
	return out
}

// stubRepo wraps the real repository, overriding single methods to fail or to
// record their arguments.
type stubRepo struct {
	domain.Repository
	saveErr   error
	existsErr error
	listLimit int
	listOff   int
}

func (r *stubRepo) Save(ctx context.Context, u *domain.User) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.Repository.Save(ctx, u)
}

func (r *stubRepo) ExistsByEmail(ctx context.Context, e domain.Email) (bool, error) {
	if r.existsErr != nil {
		return false, r.existsErr
	}
	return r.Repository.ExistsByEmail(ctx, e)
}

func (r *stubRepo) List(ctx context.Context, limit, offset int) ([]*domain.User, error) {
	r.listLimit, r.listOff = limit, offset
	return r.Repository.List(ctx, limit, offset)
}

func newRepo() *stubRepo { return &stubRepo{Repository: memory.NewUserRepository()} }

// newService wires the service over a fresh repository and publisher.
func newService(t *testing.T) (*app.Service, *stubRepo, *recordingPublisher) {
	t.Helper()
	repo := newRepo()
	pub := &recordingPublisher{}
	return app.NewService(repo, pub, discardLogger()), repo, pub
}

// register is a fixture helper.
func register(t *testing.T, s *app.Service, name, email string) app.UserView {
	t.Helper()
	v, err := s.Register(context.Background(), app.RegisterCommand{Name: name, Email: email})
	if err != nil {
		t.Fatalf("Register(%q): %v", email, err)
	}
	return v
}

func TestRegister(t *testing.T) {
	ctx := context.Background()

	t.Run("persists an inactive user and dispatches the event", func(t *testing.T) {
		svc, repo, pub := newService(t)

		got, err := svc.Register(ctx, app.RegisterCommand{Name: "Alice", Email: "  Alice@Example.COM "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Email != "alice@example.com" {
			t.Errorf("email should be normalised: got %q", got.Email)
		}
		if got.Active {
			t.Error("a newly registered user must be inactive")
		}

		stored, err := repo.FindByID(ctx, mustID(t, got.ID))
		if err != nil {
			t.Fatalf("the user should have been saved: %v", err)
		}
		if stored.Name() != "Alice" {
			t.Errorf("stored name: got %q, want %q", stored.Name(), "Alice")
		}
		if names := pub.names(); len(names) != 1 || names[0] != "user.registered" {
			t.Errorf("dispatched events: got %v, want [user.registered]", names)
		}
	})

	t.Run("rejects an email already registered", func(t *testing.T) {
		svc, _, _ := newService(t)
		register(t, svc, "Alice", "alice@example.com")

		_, err := svc.Register(ctx, app.RegisterCommand{Name: "Bob", Email: "alice@example.com"})
		if !errors.Is(err, domain.ErrEmailTaken) {
			t.Fatalf("got %v, want ErrEmailTaken", err)
		}
	})

	t.Run("rejects a malformed email before touching the repository", func(t *testing.T) {
		svc, _, pub := newService(t)

		if _, err := svc.Register(ctx, app.RegisterCommand{Name: "Alice", Email: "not-an-email"}); !errors.Is(err, domain.ErrEmailInvalid) {
			t.Fatalf("got %v, want ErrEmailInvalid", err)
		}
		if len(pub.events) != 0 {
			t.Errorf("nothing should have been dispatched, got %v", pub.names())
		}
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		svc, _, _ := newService(t)
		if _, err := svc.Register(ctx, app.RegisterCommand{Name: "   ", Email: "a@example.com"}); !errors.Is(err, domain.ErrNameEmpty) {
			t.Fatalf("got %v, want ErrNameEmpty", err)
		}
	})

	t.Run("propagates a lookup failure", func(t *testing.T) {
		boom := errors.New("database is unreachable")
		repo := newRepo()
		repo.existsErr = boom
		svc := app.NewService(repo, &recordingPublisher{}, discardLogger())

		if _, err := svc.Register(ctx, app.RegisterCommand{Name: "Alice", Email: "a@example.com"}); !errors.Is(err, boom) {
			t.Fatalf("got %v, want the repository error", err)
		}
	})

	t.Run("propagates a save failure", func(t *testing.T) {
		boom := errors.New("disk is full")
		repo := newRepo()
		repo.saveErr = boom
		pub := &recordingPublisher{}
		svc := app.NewService(repo, pub, discardLogger())

		if _, err := svc.Register(ctx, app.RegisterCommand{Name: "Alice", Email: "a@example.com"}); !errors.Is(err, boom) {
			t.Fatalf("got %v, want the repository error", err)
		}
		if len(pub.events) != 0 {
			t.Errorf("no event may be dispatched for a change that failed to persist, got %v", pub.names())
		}
	})
}

func TestActivationAndDeactivation(t *testing.T) {
	ctx := context.Background()
	svc, _, pub := newService(t)
	created := register(t, svc, "Alice", "alice@example.com")

	got, err := svc.Activate(ctx, created.ID)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !got.Active {
		t.Error("the user should be active")
	}

	if _, err := svc.Activate(ctx, created.ID); !errors.Is(err, domain.ErrAlreadyActive) {
		t.Errorf("re-activating: got %v, want ErrAlreadyActive", err)
	}

	if _, err := svc.Deactivate(ctx, created.ID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if _, err := svc.Deactivate(ctx, created.ID); !errors.Is(err, domain.ErrAlreadyInactive) {
		t.Errorf("re-deactivating: got %v, want ErrAlreadyInactive", err)
	}

	want := []string{"user.registered", "user.activated", "user.deactivated"}
	if got := pub.names(); len(got) != len(want) {
		t.Fatalf("dispatched events: got %v, want %v", got, want)
	}
	for i, name := range want {
		if pub.names()[i] != name {
			t.Errorf("event %d: got %q, want %q", i, pub.names()[i], name)
		}
	}
}

func TestRename(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	created := register(t, svc, "Alice", "alice@example.com")

	got, err := svc.Rename(ctx, app.RenameCommand{ID: created.ID, Name: "  Alice Cooper "})
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got.Name != "Alice Cooper" {
		t.Errorf("name should be trimmed: got %q", got.Name)
	}

	if _, err := svc.Rename(ctx, app.RenameCommand{ID: created.ID, Name: "  "}); !errors.Is(err, domain.ErrNameEmpty) {
		t.Errorf("empty name: got %v, want ErrNameEmpty", err)
	}
}

func TestMutateOnMissingOrMalformedID(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)

	for name, id := range map[string]string{
		"malformed id": "not-a-uuid",
		"unknown id":   domain.NewID().String(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.Activate(ctx, id); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("Activate: got %v, want ErrNotFound", err)
			}
			if _, err := svc.GetByID(ctx, id); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("GetByID: got %v, want ErrNotFound", err)
			}
			if err := svc.Delete(ctx, id); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("Delete: got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestGetByIDAndDelete(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	created := register(t, svc, "Alice", "alice@example.com")

	got, err := svc.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("id: got %q, want %q", got.ID, created.ID)
	}

	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.GetByID(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("after Delete: got %v, want ErrNotFound", err)
	}
}

func TestListClampsPagination(t *testing.T) {
	// The service, not the transport, is what guarantees a caller cannot ask for
	// an unbounded page. The stub records the arguments it actually received.
	ctx := context.Background()

	cases := []struct {
		name                  string
		limit, offset         int
		wantLimit, wantOffset int
	}{
		{"a zero limit falls back to the default", 0, 0, 20, 0},
		{"a negative limit falls back to the default", -5, 0, 20, 0},
		{"an excessive limit falls back to the default", 1000, 0, 20, 0},
		{"a negative offset is clamped to zero", 10, -3, 10, 0},
		{"a sane page is passed through", 50, 100, 50, 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo()
			svc := app.NewService(repo, &recordingPublisher{}, discardLogger())

			if _, err := svc.List(ctx, tc.limit, tc.offset); err != nil {
				t.Fatalf("List: %v", err)
			}
			if repo.listLimit != tc.wantLimit {
				t.Errorf("limit passed to the repository: got %d, want %d", repo.listLimit, tc.wantLimit)
			}
			if repo.listOff != tc.wantOffset {
				t.Errorf("offset passed to the repository: got %d, want %d", repo.listOff, tc.wantOffset)
			}
		})
	}

	t.Run("returns a view per user, never nil", func(t *testing.T) {
		svc, _, _ := newService(t)
		register(t, svc, "Alice", "alice@example.com")
		register(t, svc, "Bob", "bob@example.com")

		got, err := svc.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got == nil {
			t.Fatal("got nil, want a non-nil slice")
		}
		if len(got) != 2 {
			t.Fatalf("got %d views, want 2", len(got))
		}
	})
}

func TestAFailedDispatchDoesNotUndoAPersistedChange(t *testing.T) {
	// Deliberate design: the write is already committed by the time events are
	// published, so a bus failure is logged, not returned. Surfacing it would
	// tell the caller their change failed when it did not.
	ctx := context.Background()
	repo := newRepo()
	pub := &recordingPublisher{err: errors.New("the bus is down")}
	svc := app.NewService(repo, pub, discardLogger())

	got, err := svc.Register(ctx, app.RegisterCommand{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("Register must succeed despite the publisher failing: %v", err)
	}
	if _, err := repo.FindByID(ctx, mustID(t, got.ID)); err != nil {
		t.Errorf("the user should still be persisted: %v", err)
	}
}

func TestANilPublisherIsTolerated(t *testing.T) {
	ctx := context.Background()
	svc := app.NewService(newRepo(), nil, discardLogger())

	if _, err := svc.Register(ctx, app.RegisterCommand{Name: "Alice", Email: "alice@example.com"}); err != nil {
		t.Fatalf("Register with no publisher wired: %v", err)
	}
}

func TestIsValidationError(t *testing.T) {
	// This table is what the HTTP layer consults to choose between 400 and 500,
	// so a missing entry silently turns a client mistake into a server error.
	validation := []error{
		domain.ErrEmailEmpty,
		domain.ErrEmailInvalid,
		domain.ErrNameEmpty,
		domain.ErrAlreadyActive,
		domain.ErrAlreadyInactive,
	}
	for _, err := range validation {
		if !app.IsValidationError(err) {
			t.Errorf("%v should be classed as a validation error", err)
		}
	}

	notValidation := []error{
		domain.ErrNotFound,
		domain.ErrEmailTaken,
		errors.New("database is unreachable"),
		nil,
	}
	for _, err := range notValidation {
		if app.IsValidationError(err) {
			t.Errorf("%v should NOT be classed as a validation error", err)
		}
	}
}

func mustID(t *testing.T, s string) domain.ID {
	t.Helper()
	id, err := domain.IDFromString(s)
	if err != nil {
		t.Fatalf("IDFromString(%q): %v", s, err)
	}
	return id
}
