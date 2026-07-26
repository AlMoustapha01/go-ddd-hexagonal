package postgres_test

import (
	"testing"

	"github.com/example/go-ddd-hex/internal/adapters/contract"
	pg "github.com/example/go-ddd-hex/internal/adapters/outbound/postgres"
	apporder "github.com/example/go-ddd-hex/internal/application/order"
	domainaccount "github.com/example/go-ddd-hex/internal/domain/account"
	domainorder "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
	domainuser "github.com/example/go-ddd-hex/internal/domain/user"
)

// The Postgres adapters run exactly the same port contract as the in-memory
// ones (see ../memory/contract_test.go). If the two implementations ever drift,
// this is where it shows.

func TestUserRepositoryContract(t *testing.T) {
	contract.UserRepository(t, func(t *testing.T) domainuser.Repository {
		return pg.NewUserRepository(freshDB(t))
	})
}

func TestOrderRepositoryContract(t *testing.T) {
	contract.OrderRepository(t, func(t *testing.T) domainorder.Repository {
		return pg.NewOrderRepository(freshDB(t))
	})
}

func TestOrderReadStoreContract(t *testing.T) {
	contract.OrderReadStore(t, func(t *testing.T) apporder.ReadStore {
		return pg.NewOrderReadStore(freshDB(t))
	})
}

func TestAccountEventStoreContract(t *testing.T) {
	contract.AccountEventStore(t, func(t *testing.T) domainaccount.EventStore {
		return pg.NewAccountEventStore(freshDB(t))
	})
}

func TestOutboxStoreContract(t *testing.T) {
	contract.OutboxStore(t, func(t *testing.T) shared.OutboxStore {
		return pg.NewOutboxStore(freshDB(t))
	})
}
