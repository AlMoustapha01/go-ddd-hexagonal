package contract

import (
	"context"
	"errors"
	"testing"

	user "github.com/example/go-ddd-hex/internal/domain/user"
)

// UserRepository runs the contract every user.Repository implementation must
// satisfy. newRepo must return a fresh, empty repository on every call.
//
// KNOWN UNDER-SPECIFICATION — email uniqueness. Postgres carries a UNIQUE
// constraint on users.email; the in-memory adapter carries none and will happily
// store two users sharing an address. The port promises nothing about
// uniqueness — the application layer guards it with ExistsByEmail — so this
// contract deliberately does not assert it. Tightening the port would mean
// surfacing ErrEmailTaken from Postgres and adding an index in memory: a
// behaviour change, not a test.
func UserRepository(t *testing.T, newRepo func(*testing.T) user.Repository) {
	ctx := context.Background()

	t.Run("Save then FindByID round-trips every field", func(t *testing.T) {
		repo := newRepo(t)
		want := newUser(t, "Alice", "alice@example.com", at(0))

		if err := repo.Save(ctx, want); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := repo.FindByID(ctx, want.ID())
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}

		if !got.ID().Equals(want.ID()) {
			t.Errorf("id: got %s, want %s", got.ID(), want.ID())
		}
		if got.Name() != want.Name() {
			t.Errorf("name: got %q, want %q", got.Name(), want.Name())
		}
		if !got.Email().Equals(want.Email()) {
			t.Errorf("email: got %s, want %s", got.Email(), want.Email())
		}
		if got.IsActive() != want.IsActive() {
			t.Errorf("active: got %v, want %v", got.IsActive(), want.IsActive())
		}
		// Equal, not ==: Postgres hands the timestamp back in its own location.
		if !got.CreatedAt().Equal(want.CreatedAt()) {
			t.Errorf("createdAt: got %s, want %s", got.CreatedAt(), want.CreatedAt())
		}
	})

	t.Run("Save updates an existing user in place", func(t *testing.T) {
		repo := newRepo(t)
		u := newUser(t, "Alice", "alice@example.com", at(0))
		if err := repo.Save(ctx, u); err != nil {
			t.Fatalf("first Save: %v", err)
		}

		renamed := user.Rehydrate(u.ID(), "Alice Cooper", u.Email(), true, u.CreatedAt(), at(1))
		if err := repo.Save(ctx, renamed); err != nil {
			t.Fatalf("second Save: %v", err)
		}

		got, err := repo.FindByID(ctx, u.ID())
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if got.Name() != "Alice Cooper" {
			t.Errorf("name: got %q, want %q", got.Name(), "Alice Cooper")
		}
		if !got.IsActive() {
			t.Error("active: got false, want true")
		}

		all, err := repo.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(all) != 1 {
			t.Errorf("an update must not create a second row: got %d users, want 1", len(all))
		}
	})

	t.Run("FindByID reports ErrNotFound for an unknown id", func(t *testing.T) {
		repo := newRepo(t)
		if _, err := repo.FindByID(ctx, user.NewID()); !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("got %v, want ErrNotFound", err)
		}
	})

	t.Run("FindByEmail finds a stored user, ErrNotFound otherwise", func(t *testing.T) {
		repo := newRepo(t)
		u := newUser(t, "Alice", "alice@example.com", at(0))
		if err := repo.Save(ctx, u); err != nil {
			t.Fatalf("Save: %v", err)
		}

		got, err := repo.FindByEmail(ctx, u.Email())
		if err != nil {
			t.Fatalf("FindByEmail: %v", err)
		}
		if !got.ID().Equals(u.ID()) {
			t.Errorf("got id %s, want %s", got.ID(), u.ID())
		}

		if _, err := repo.FindByEmail(ctx, mustEmail(t, "nobody@example.com")); !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("unknown email: got %v, want ErrNotFound", err)
		}
	})

	t.Run("ExistsByEmail reflects what is stored", func(t *testing.T) {
		repo := newRepo(t)
		u := newUser(t, "Alice", "alice@example.com", at(0))

		exists, err := repo.ExistsByEmail(ctx, u.Email())
		if err != nil {
			t.Fatalf("ExistsByEmail before save: %v", err)
		}
		if exists {
			t.Error("got true before the user was saved, want false")
		}

		if err := repo.Save(ctx, u); err != nil {
			t.Fatalf("Save: %v", err)
		}

		exists, err = repo.ExistsByEmail(ctx, u.Email())
		if err != nil {
			t.Fatalf("ExistsByEmail after save: %v", err)
		}
		if !exists {
			t.Error("got false after the user was saved, want true")
		}
	})

	t.Run("List returns users newest first", func(t *testing.T) {
		repo := newRepo(t)
		oldest := newUser(t, "Oldest", "oldest@example.com", at(0))
		middle := newUser(t, "Middle", "middle@example.com", at(1))
		newest := newUser(t, "Newest", "newest@example.com", at(2))
		for _, u := range []*user.User{middle, newest, oldest} { // insertion order is not sort order
			if err := repo.Save(ctx, u); err != nil {
				t.Fatalf("Save: %v", err)
			}
		}

		got, err := repo.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		want := []string{"Newest", "Middle", "Oldest"}
		if len(got) != len(want) {
			t.Fatalf("got %d users, want %d", len(got), len(want))
		}
		for i, name := range want {
			if got[i].Name() != name {
				t.Errorf("position %d: got %q, want %q", i, got[i].Name(), name)
			}
		}
	})

	t.Run("List honours limit and offset", func(t *testing.T) {
		repo := newRepo(t)
		for i, name := range []string{"A", "B", "C"} {
			u := newUser(t, name, name+"@example.com", at(i))
			if err := repo.Save(ctx, u); err != nil {
				t.Fatalf("Save: %v", err)
			}
		}

		page, err := repo.List(ctx, 1, 1) // second newest: "B"
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page) != 1 {
			t.Fatalf("got %d users, want 1", len(page))
		}
		if page[0].Name() != "B" {
			t.Errorf("got %q, want %q", page[0].Name(), "B")
		}
	})

	t.Run("List returns a non-nil empty slice when there is nothing", func(t *testing.T) {
		// A list method that finds nothing must return [], never nil: the same
		// API would otherwise serialise "null" on one adapter and "[]" on the
		// other.
		repo := newRepo(t)
		got, err := repo.List(ctx, 10, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got == nil {
			t.Fatal("got nil, want a non-nil empty slice")
		}
		if len(got) != 0 {
			t.Fatalf("got %d users, want 0", len(got))
		}
	})

	t.Run("List past the end returns a non-nil empty slice", func(t *testing.T) {
		repo := newRepo(t)
		if err := repo.Save(ctx, newUser(t, "Alice", "alice@example.com", at(0))); err != nil {
			t.Fatalf("Save: %v", err)
		}
		got, err := repo.List(ctx, 10, 50)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got == nil {
			t.Fatal("got nil, want a non-nil empty slice")
		}
		if len(got) != 0 {
			t.Fatalf("got %d users, want 0", len(got))
		}
	})

	t.Run("Delete removes the user, and deleting twice reports ErrNotFound", func(t *testing.T) {
		repo := newRepo(t)
		u := newUser(t, "Alice", "alice@example.com", at(0))
		if err := repo.Save(ctx, u); err != nil {
			t.Fatalf("Save: %v", err)
		}

		if err := repo.Delete(ctx, u.ID()); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := repo.FindByID(ctx, u.ID()); !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("FindByID after Delete: got %v, want ErrNotFound", err)
		}
		if err := repo.Delete(ctx, u.ID()); !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("second Delete: got %v, want ErrNotFound", err)
		}
	})
}
