// Package bootstrap is the COMPOSITION ROOT. It is the one place allowed to
// know about every concrete adapter, and it wires them together according to
// configuration. Nothing else in the codebase imports concrete adapters across
// layer boundaries — dependencies flow inward through ports, and this file is
// where the arrows are finally connected.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/example/go-ddd-hex/internal/adapters/inbound/http"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/eventbus"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/outbox"
	pg "github.com/example/go-ddd-hex/internal/adapters/outbound/postgres"
	appaccount "github.com/example/go-ddd-hex/internal/application/account"
	"github.com/example/go-ddd-hex/internal/application/integration"
	apporder "github.com/example/go-ddd-hex/internal/application/order"
	appuser "github.com/example/go-ddd-hex/internal/application/user"
	domainaccount "github.com/example/go-ddd-hex/internal/domain/account"
	domainorder "github.com/example/go-ddd-hex/internal/domain/order"
	"github.com/example/go-ddd-hex/internal/domain/shared"
	domainuser "github.com/example/go-ddd-hex/internal/domain/user"
	"github.com/example/go-ddd-hex/internal/platform/config"
)

// App holds the assembled server and any resources that must be closed on
// shutdown.
type App struct {
	Server *http.Server

	pool  *pgxpool.Pool
	relay *outbox.Relay
}

// New builds the application graph from configuration.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	var (
		userRepo       domainuser.Repository
		orderRepo      domainorder.Repository
		orderReadStore apporder.ReadStore
		outboxStore    shared.OutboxStore
		uow            shared.Atomic
		accountStore   domainaccount.EventStore
		pool           *pgxpool.Pool
	)

	// Select the outbound persistence adapters. The application layer is
	// oblivious to which implementation is chosen. Note the CQRS split (write
	// repo + read store) and the Outbox infrastructure (outbox store + unit of
	// work) per driver.
	switch cfg.Driver {
	case "postgres":
		p, err := pg.NewPool(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: %w", err)
		}
		pool = p
		userRepo = pg.NewUserRepository(p)
		orderRepo = pg.NewOrderRepository(p)
		orderReadStore = pg.NewOrderReadStore(p)
		outboxStore = pg.NewOutboxStore(p)
		uow = pg.NewTransactor(p)
		accountStore = pg.NewAccountEventStore(p)
		log.Info("using postgres repositories")
	default:
		userRepo = memory.NewUserRepository()
		orderRepo = memory.NewOrderRepository()
		orderReadStore = memory.NewOrderReadStore()
		outboxStore = memory.NewOutboxStore()
		uow = memory.NewTransactor()
		accountStore = memory.NewAccountEventStore()
		log.Info("using in-memory repositories")
	}

	// In-process event bus. Handlers subscribe here to project and to coordinate
	// across aggregates. Order events reach it via the outbox relay; user events
	// are published directly (see note below).
	bus := eventbus.New(log)

	// Outbox: the order command side writes events to this store transactionally,
	// and the relay forwards them to the bus (at-least-once).
	outboxPublisher := outbox.NewPublisher(outboxStore)
	relay := outbox.NewRelay(outboxStore, bus, log)

	// User command use cases. NOTE: the user context publishes DIRECTLY to the
	// bus (no outbox) on purpose, to contrast with the order context — it still
	// carries the dual-write risk the outbox removes. Wiring it through the
	// outbox would be identical to the order context below.
	userUC := appuser.NewService(userRepo, bus, log)

	// Order COMMAND side: writes aggregate + events atomically via the unit of
	// work and the outbox publisher, and uses the relay as its dispatch hint.
	customerChecker := integration.NewUserCustomerChecker(userRepo)
	orderUC := apporder.NewService(orderRepo, customerChecker, uow, outboxPublisher, relay, log)

	// Order QUERY side (CQRS): the projector subscribes to relayed order events
	// and keeps the read store in sync; the query service reads only from it.
	projector := apporder.NewProjector(orderRepo, orderReadStore, log)
	for _, name := range projector.EventNames() {
		bus.Subscribe(name, projector.Handle)
	}
	orderQueries := apporder.NewQueryService(orderReadStore)

	// Cross-aggregate reaction: when a user is deactivated, cancel their open
	// orders. It reads open orders from the query side and cancels via commands.
	cancelOnDeactivation := integration.NewCancelOrdersOnUserDeactivation(orderUC, orderQueries, log)
	bus.Subscribe("user.deactivated", cancelOnDeactivation.Handle)

	// Account context (EVENT SOURCING): command + query services over the event
	// store. Self-contained — state is derived by replaying events.
	accountUC := appaccount.NewService(accountStore, log)
	accountQueries := appaccount.NewQueryService(accountStore)

	// Start the outbox poller (durability backstop). The command side also
	// dispatches inline for low latency; this catches anything left by a crash.
	relay.Start(ctx, 2*time.Second)

	// Driving adapter.
	server := httpadapter.NewServer(cfg.HTTPAddr, httpadapter.ServerDeps{
		UserUC:         userUC,
		OrderUC:        orderUC,
		OrderQueries:   orderQueries,
		AccountUC:      accountUC,
		AccountQueries: accountQueries,
	}, log)

	return &App{Server: server, pool: pool, relay: relay}, nil
}

// Close releases resources held by adapters.
func (a *App) Close() {
	if a.relay != nil {
		a.relay.Stop()
	}
	if a.pool != nil {
		a.pool.Close()
	}
}
