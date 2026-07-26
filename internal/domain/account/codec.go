package account

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/go-ddd-hex/internal/domain/shared"
)

// The codec serializes events for the event store and reconstructs the concrete
// types on load. Event sourcing lives or dies by this: stored events must be
// faithfully reconstructable, potentially years later, so the payload is an
// explicit, stable shape — never the raw Go struct (whose fields may change).

type eventDTO struct {
	AccountID  string    `json:"account_id"`
	Amount     int64     `json:"amount,omitempty"`
	Currency   string    `json:"currency,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Encode serializes an event to its name and JSON payload.
func Encode(e shared.Event) (name string, payload []byte, err error) {
	var dto eventDTO
	switch ev := e.(type) {
	case Opened:
		dto = eventDTO{AccountID: ev.AccountID.String(), Currency: ev.Currency, OccurredAt: ev.OccurredAt()}
	case Deposited:
		dto = eventDTO{AccountID: ev.AccountID.String(), Amount: ev.Amount, Currency: ev.Currency, OccurredAt: ev.OccurredAt()}
	case Withdrawn:
		dto = eventDTO{AccountID: ev.AccountID.String(), Amount: ev.Amount, Currency: ev.Currency, OccurredAt: ev.OccurredAt()}
	default:
		return "", nil, fmt.Errorf("%w: %T", ErrUnknownEvent, e)
	}
	payload, err = json.Marshal(dto)
	if err != nil {
		return "", nil, err
	}
	return e.EventName(), payload, nil
}

// Decode reconstructs a concrete event from its name and payload.
func Decode(name string, payload []byte) (shared.Event, error) {
	var dto eventDTO
	if err := json.Unmarshal(payload, &dto); err != nil {
		return nil, fmt.Errorf("account: decode payload: %w", err)
	}
	id, err := IDFromString(dto.AccountID)
	if err != nil {
		return nil, fmt.Errorf("account: decode id: %w", err)
	}
	base := newBase(dto.OccurredAt)
	switch name {
	case "account.opened":
		return Opened{BaseEvent: base, AccountID: id, Currency: dto.Currency}, nil
	case "account.deposited":
		return Deposited{BaseEvent: base, AccountID: id, Amount: dto.Amount, Currency: dto.Currency}, nil
	case "account.withdrawn":
		return Withdrawn{BaseEvent: base, AccountID: id, Amount: dto.Amount, Currency: dto.Currency}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownEvent, name)
	}
}
