package http_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	adapter "github.com/example/go-ddd-hex/internal/adapters/inbound/http"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/eventbus"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/memory"
	"github.com/example/go-ddd-hex/internal/adapters/outbound/outbox"
	appaccount "github.com/example/go-ddd-hex/internal/application/account"
	"github.com/example/go-ddd-hex/internal/application/integration"
	apporder "github.com/example/go-ddd-hex/internal/application/order"
	app "github.com/example/go-ddd-hex/internal/application/user"
)

// This test wires the real HTTP adapter to the real use cases over the
// in-memory repositories — an end-to-end slice through every layer, no mocks
// and no database. That is exactly what the port abstractions buy you.
func newTestServer() http.Handler {
	log := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	bus := eventbus.New(log)
	userRepo := memory.NewUserRepository()
	orderRepo := memory.NewOrderRepository()
	orderReadStore := memory.NewOrderReadStore()

	// Outbox: transactional publisher + relay (drained inline by the command side).
	outboxStore := memory.NewOutboxStore()
	uow := memory.NewTransactor()
	outboxPublisher := outbox.NewPublisher(outboxStore)
	relay := outbox.NewRelay(outboxStore, bus, log)

	userUC := app.NewService(userRepo, bus, log)
	orderUC := apporder.NewService(orderRepo, integration.NewUserCustomerChecker(userRepo), uow, outboxPublisher, relay, log)

	// CQRS read side: projector + query service.
	projector := apporder.NewProjector(orderRepo, orderReadStore, log)
	for _, name := range projector.EventNames() {
		bus.Subscribe(name, projector.Handle)
	}
	orderQueries := apporder.NewQueryService(orderReadStore)

	bus.Subscribe("user.deactivated",
		integration.NewCancelOrdersOnUserDeactivation(orderUC, orderQueries, log).Handle)

	// Event-sourced account context.
	accountStore := memory.NewAccountEventStore()
	accountUC := appaccount.NewService(accountStore, log)
	accountQueries := appaccount.NewQueryService(accountStore)

	return adapter.NewServer(":0", adapter.ServerDeps{
		UserUC:         userUC,
		OrderUC:        orderUC,
		OrderQueries:   orderQueries,
		AccountUC:      accountUC,
		AccountQueries: accountQueries,
	}, log).Handler
}

func TestRegisterAndActivateFlow(t *testing.T) {
	srv := newTestServer()

	// Register.
	body, _ := json.Marshal(map[string]string{"name": "Alice", "email": "alice@example.com"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: got %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	var created app.UserView
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.Active {
		t.Error("new user should be inactive")
	}

	// Activate.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/users/"+created.ID+"/activate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	// Duplicate email => 409.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate: got %d, want 409", rec.Code)
	}
}

func TestGetUnknownUserReturns404(t *testing.T) {
	srv := newTestServer()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/3f2504e0-4f89-41d3-9a0c-0305e82c3301", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.Code)
	}
}

func TestRegisterInvalidEmailReturns400(t *testing.T) {
	srv := newTestServer()
	body, _ := json.Marshal(map[string]string{"name": "Bob", "email": "nope"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}
