package account

import (
	"time"

	domain "github.com/example/go-ddd-hex/internal/domain/account"
)

// OpenCommand opens a new account in a currency.
type OpenCommand struct {
	Currency string
}

// AmountCommand is a deposit or withdrawal on an account.
type AmountCommand struct {
	AccountID string
	Amount    int64
}

// AccountView is the current state, derived by folding the event stream.
type AccountView struct {
	ID           string `json:"id"`
	Currency     string `json:"currency"`
	BalanceMinor int64  `json:"balance_minor"`
	Version      int    `json:"version"`
}

// HistoryEntry is one event in an account's audit trail — a capability you get
// for free with event sourcing.
type HistoryEntry struct {
	Sequence    int       `json:"sequence"`
	Event       string    `json:"event"`
	AmountMinor int64     `json:"amount_minor,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}

func toView(a *domain.Account) AccountView {
	return AccountView{
		ID:           a.ID().String(),
		Currency:     a.Currency(),
		BalanceMinor: a.Balance(),
		Version:      a.Version(),
	}
}
