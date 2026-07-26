package memory_test

import (
	"testing"

	"github.com/example/go-ddd-hex/internal/adapters/contract"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	apporder "github.com/example/go-ddd-hex/internal/application/order"
	domainaccount "github.com/example/go-ddd-hex/internal/domain/account"
	domainorder "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
	domainuser "github.com/example/go-ddd-hex/internal/domain/user"
)

// The in-memory adapters run exactly the same port contract as the Postgres
// ones (see ../postgres/contract_test.go). Keeping both honest against a single
// suite is what makes them genuinely interchangeable rather than merely
// similar-looking.

func TestUserRepositoryContract(t *testing.T) {
	contract.UserRepository(t, func(*testing.T) domainuser.Repository {
		return memory.NewUserRepository()
	})
}

func TestOrderRepositoryContract(t *testing.T) {
	contract.OrderRepository(t, func(*testing.T) domainorder.Repository {
		return memory.NewOrderRepository()
	})
}

func TestOrderReadStoreContract(t *testing.T) {
	contract.OrderReadStore(t, func(*testing.T) apporder.ReadStore {
		return memory.NewOrderReadStore()
	})
}

func TestAccountEventStoreContract(t *testing.T) {
	contract.AccountEventStore(t, func(*testing.T) domainaccount.EventStore {
		return memory.NewAccountEventStore()
	})
}

func TestOutboxStoreContract(t *testing.T) {
	contract.OutboxStore(t, func(*testing.T) shared.OutboxStore {
		return memory.NewOutboxStore()
	})
}
