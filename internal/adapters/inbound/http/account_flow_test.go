package http_test

import (
	"encoding/json"
	"net/http"
	"testing"

	appaccount "github.com/example/go-ddd-hex/internal/application/account"
)

// TestAccountEventSourcingFlow drives the event-sourced account through HTTP:
// open → deposit → withdraw, checks the folded balance, and reads the full
// event history (the audit trail event sourcing gives for free).
func TestAccountEventSourcingFlow(t *testing.T) {
	srv := newTestServer()

	// Open.
	rec := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"currency": "EUR"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	var acc appaccount.AccountView
	_ = json.Unmarshal(rec.Body.Bytes(), &acc)
	if acc.Version != 1 || acc.BalanceMinor != 0 {
		t.Fatalf("after open: version=%d balance=%d, want 1/0", acc.Version, acc.BalanceMinor)
	}

	// Deposit 1000, withdraw 400 -> balance 600, version 3.
	_ = do(t, srv, http.MethodPost, "/api/v1/accounts/"+acc.ID+"/deposit", map[string]any{"amount_minor": 1000})
	rec = do(t, srv, http.MethodPost, "/api/v1/accounts/"+acc.ID+"/withdraw", map[string]any{"amount_minor": 400})
	_ = json.Unmarshal(rec.Body.Bytes(), &acc)
	if acc.BalanceMinor != 600 || acc.Version != 3 {
		t.Fatalf("after deposit/withdraw: balance=%d version=%d, want 600/3", acc.BalanceMinor, acc.Version)
	}

	// Reading state folds the stream.
	rec = do(t, srv, http.MethodGet, "/api/v1/accounts/"+acc.ID, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &acc)
	if acc.BalanceMinor != 600 {
		t.Fatalf("get balance: got %d, want 600", acc.BalanceMinor)
	}

	// History returns the full event log.
	rec = do(t, srv, http.MethodGet, "/api/v1/accounts/"+acc.ID+"/history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history: %d", rec.Code)
	}
	var history []appaccount.HistoryEntry
	_ = json.Unmarshal(rec.Body.Bytes(), &history)
	if len(history) != 3 {
		t.Fatalf("history length: got %d, want 3", len(history))
	}
	if history[0].Event != "account.opened" || history[1].Event != "account.deposited" || history[2].Event != "account.withdrawn" {
		t.Fatalf("history sequence unexpected: %+v", history)
	}
	if history[1].AmountMinor != 1000 || history[2].AmountMinor != 400 {
		t.Fatalf("history amounts: %+v", history)
	}
}

func TestAccountOverdrawRejected(t *testing.T) {
	srv := newTestServer()
	rec := do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"currency": "EUR"})
	var acc appaccount.AccountView
	_ = json.Unmarshal(rec.Body.Bytes(), &acc)

	_ = do(t, srv, http.MethodPost, "/api/v1/accounts/"+acc.ID+"/deposit", map[string]any{"amount_minor": 100})
	rec = do(t, srv, http.MethodPost, "/api/v1/accounts/"+acc.ID+"/withdraw", map[string]any{"amount_minor": 500})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("overdraw: got %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAccountNotFound(t *testing.T) {
	srv := newTestServer()
	rec := do(t, srv, http.MethodGet, "/api/v1/accounts/3f2504e0-4f89-41d3-9a0c-0305e82c3301", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.Code)
	}
}
