package contract

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// OutboxStore runs the contract every shared.OutboxStore implementation must
// satisfy. newStore must return a fresh, empty store on every call.
//
// KNOWN DIVERGENCE — a non-positive limit. FetchUnpublished(ctx, 0) returns
// everything in memory and nothing on Postgres (SQL "LIMIT 0"). The case is
// unreachable in production: the relay always passes its batchSize of 100
// (relay.go). The contract therefore only exercises a positive limit.
func OutboxStore(t *testing.T, newStore func(*testing.T) shared.OutboxStore) {
	ctx := context.Background()

	t.Run("added records come back oldest first", func(t *testing.T) {
		store := newStore(t)
		// Inserted out of chronological order on purpose: the port promises
		// "oldest first", which is a property of the records, not of the order
		// in which they happened to be written.
		newest := outboxRecord("order.paid", at(2))
		oldest := outboxRecord("order.created", at(0))
		middle := outboxRecord("order.placed", at(1))
		if err := store.Add(ctx, newest, oldest, middle); err != nil {
			t.Fatalf("Add: %v", err)
		}

		got, err := store.FetchUnpublished(ctx, 10)
		if err != nil {
			t.Fatalf("FetchUnpublished: %v", err)
		}
		want := []string{"order.created", "order.placed", "order.paid"}
		if len(got) != len(want) {
			t.Fatalf("got %d records, want %d", len(got), len(want))
		}
		for i, name := range want {
			if got[i].EventName != name {
				t.Errorf("position %d: got %q, want %q", i, got[i].EventName, name)
			}
		}
	})

	t.Run("a record round-trips every field", func(t *testing.T) {
		store := newStore(t)
		want := outboxRecord("order.created", at(0))
		if err := store.Add(ctx, want); err != nil {
			t.Fatalf("Add: %v", err)
		}

		got, err := store.FetchUnpublished(ctx, 10)
		if err != nil {
			t.Fatalf("FetchUnpublished: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d records, want 1", len(got))
		}
		if got[0].ID != want.ID {
			t.Errorf("id: got %q, want %q", got[0].ID, want.ID)
		}
		if got[0].EventName != want.EventName {
			t.Errorf("eventName: got %q, want %q", got[0].EventName, want.EventName)
		}
		if got[0].AggregateID != want.AggregateID {
			t.Errorf("aggregateID: got %q, want %q", got[0].AggregateID, want.AggregateID)
		}
		// Compared as JSON, not as bytes: Postgres stores the payload in a JSONB
		// column, which reformats whitespace and key order. What the port
		// promises is that the DOCUMENT survives, not its exact byte layout.
		if !sameJSON(t, got[0].Payload, want.Payload) {
			t.Errorf("payload: got %s, want %s", got[0].Payload, want.Payload)
		}
		if !got[0].OccurredAt.Equal(want.OccurredAt) {
			t.Errorf("occurredAt: got %s, want %s", got[0].OccurredAt, want.OccurredAt)
		}
	})

	t.Run("FetchUnpublished honours the limit", func(t *testing.T) {
		store := newStore(t)
		for i := 0; i < 5; i++ {
			if err := store.Add(ctx, outboxRecord("order.created", at(i))); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}

		got, err := store.FetchUnpublished(ctx, 2)
		if err != nil {
			t.Fatalf("FetchUnpublished: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d records, want 2", len(got))
		}
	})

	t.Run("MarkPublished keeps records out of later fetches", func(t *testing.T) {
		store := newStore(t)
		first := outboxRecord("order.created", at(0))
		second := outboxRecord("order.placed", at(1))
		if err := store.Add(ctx, first, second); err != nil {
			t.Fatalf("Add: %v", err)
		}

		if err := store.MarkPublished(ctx, first.ID); err != nil {
			t.Fatalf("MarkPublished: %v", err)
		}

		got, err := store.FetchUnpublished(ctx, 10)
		if err != nil {
			t.Fatalf("FetchUnpublished: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d records, want 1", len(got))
		}
		if got[0].ID != second.ID {
			t.Errorf("got %q, want the unpublished record %q", got[0].ID, second.ID)
		}
	})

	t.Run("MarkPublished with no ids is a no-op", func(t *testing.T) {
		store := newStore(t)
		if err := store.Add(ctx, outboxRecord("order.created", at(0))); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := store.MarkPublished(ctx); err != nil {
			t.Fatalf("MarkPublished(): %v", err)
		}
		got, err := store.FetchUnpublished(ctx, 10)
		if err != nil {
			t.Fatalf("FetchUnpublished: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("got %d records, want 1 — nothing should have been marked", len(got))
		}
	})

	t.Run("an empty outbox yields a non-nil empty slice", func(t *testing.T) {
		store := newStore(t)
		got, err := store.FetchUnpublished(ctx, 10)
		if err != nil {
			t.Fatalf("FetchUnpublished: %v", err)
		}
		if got == nil {
			t.Fatal("got nil, want a non-nil empty slice")
		}
		if len(got) != 0 {
			t.Fatalf("got %d records, want 0", len(got))
		}
	})

	t.Run("Add with no records is a no-op", func(t *testing.T) {
		store := newStore(t)
		if err := store.Add(ctx); err != nil {
			t.Fatalf("Add(): %v", err)
		}
		got, err := store.FetchUnpublished(ctx, 10)
		if err != nil {
			t.Fatalf("FetchUnpublished: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("got %d records, want 0", len(got))
		}
	})
}

// sameJSON reports whether two payloads carry the same JSON document.
func sameJSON(t *testing.T, got, want []byte) bool {
	t.Helper()
	var gotDoc, wantDoc any
	if err := json.Unmarshal(got, &gotDoc); err != nil {
		t.Fatalf("stored payload is not valid JSON (%s): %v", got, err)
	}
	if err := json.Unmarshal(want, &wantDoc); err != nil {
		t.Fatalf("fixture payload is not valid JSON (%s): %v", want, err)
	}
	return reflect.DeepEqual(gotDoc, wantDoc)
}

// outboxRecord builds a record fixture. The aggregate id must be a real UUID
// (the Postgres column is typed UUID) and the payload must be valid JSON (the
// column is JSONB).
func outboxRecord(eventName string, occurredAt time.Time) shared.OutboxRecord {
	return shared.OutboxRecord{
		ID:          uuid.Must(uuid.NewV7()).String(),
		EventName:   eventName,
		AggregateID: uuid.Must(uuid.NewV7()).String(),
		Payload:     []byte(`{"event":"` + eventName + `"}`),
		OccurredAt:  occurredAt,
	}
}
