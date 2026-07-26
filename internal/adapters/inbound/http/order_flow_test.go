package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apporder "github.com/example/go-ddd-hex/internal/application/order"
	appuser "github.com/example/go-ddd-hex/internal/application/user"
)

func do(t *testing.T, srv http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(method, path, rdr))
	return rec
}

// registerActiveUser creates a user and activates it, returning its id.
func registerActiveUser(t *testing.T, srv http.Handler, email string) string {
	t.Helper()
	rec := do(t, srv, http.MethodPost, "/api/v1/users", map[string]string{"name": "C", "email": email})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var u appuser.UserView
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if rec := do(t, srv, http.MethodPost, "/api/v1/users/"+u.ID+"/activate", nil); rec.Code != http.StatusOK {
		t.Fatalf("activate: %d", rec.Code)
	}
	return u.ID
}

func TestOrderLifecycle(t *testing.T) {
	srv := newTestServer()
	uid := registerActiveUser(t, srv, "buyer@example.com")

	// Create a draft order.
	rec := do(t, srv, http.MethodPost, "/api/v1/orders", map[string]any{"customer_id": uid, "currency": "EUR"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order: %d %s", rec.Code, rec.Body.String())
	}
	var o apporder.OrderView
	_ = json.Unmarshal(rec.Body.Bytes(), &o)

	// Add a line, place, pay.
	rec = do(t, srv, http.MethodPost, "/api/v1/orders/"+o.ID+"/lines",
		map[string]any{"product_id": "SKU-1", "unit_price_minor": 1500, "currency": "EUR", "quantity": 3})
	if rec.Code != http.StatusOK {
		t.Fatalf("add line: %d %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &o)
	if o.TotalMinor != 4500 {
		t.Errorf("total: got %d, want 4500", o.TotalMinor)
	}

	if rec := do(t, srv, http.MethodPost, "/api/v1/orders/"+o.ID+"/place", nil); rec.Code != http.StatusOK {
		t.Fatalf("place: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/v1/orders/"+o.ID+"/pay", nil); rec.Code != http.StatusOK {
		t.Fatalf("pay: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrderForInactiveCustomerRejected(t *testing.T) {
	srv := newTestServer()
	// Register but DO NOT activate.
	rec := do(t, srv, http.MethodPost, "/api/v1/users", map[string]string{"name": "X", "email": "inactive@example.com"})
	var u appuser.UserView
	_ = json.Unmarshal(rec.Body.Bytes(), &u)

	rec = do(t, srv, http.MethodPost, "/api/v1/orders", map[string]any{"customer_id": u.ID, "currency": "EUR"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for inactive customer, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestCQRSReadModelReflectsWrites checks the CQRS loop end to end: writes go
// through the command side, a projector updates the separate read model from
// domain events, and the query side (GET) returns the projection.
func TestCQRSReadModelReflectsWrites(t *testing.T) {
	srv := newTestServer()
	uid := registerActiveUser(t, srv, "cqrs@example.com")

	// Command: create draft. The projector should immediately populate the read model.
	rec := do(t, srv, http.MethodPost, "/api/v1/orders", map[string]any{"customer_id": uid, "currency": "EUR"})
	var created apporder.OrderView
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Query: GET returns the read-model DetailView (note item_count, a read-only field).
	rec = do(t, srv, http.MethodGet, "/api/v1/orders/"+created.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query draft: %d %s", rec.Code, rec.Body.String())
	}
	var detail apporder.DetailView
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail.Status != "draft" || detail.ItemCount != 0 {
		t.Fatalf("read model after create: status=%q item_count=%d, want draft/0", detail.Status, detail.ItemCount)
	}

	// Command: add two lines.
	_ = do(t, srv, http.MethodPost, "/api/v1/orders/"+created.ID+"/lines",
		map[string]any{"product_id": "SKU-1", "unit_price_minor": 1000, "currency": "EUR", "quantity": 2})
	_ = do(t, srv, http.MethodPost, "/api/v1/orders/"+created.ID+"/lines",
		map[string]any{"product_id": "SKU-2", "unit_price_minor": 500, "currency": "EUR", "quantity": 1})

	// Query: read model reflects the projected total and item count.
	rec = do(t, srv, http.MethodGet, "/api/v1/orders/"+created.ID, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail.TotalMinor != 2500 || detail.ItemCount != 2 {
		t.Fatalf("read model after lines: total=%d item_count=%d, want 2500/2", detail.TotalMinor, detail.ItemCount)
	}

	// Query: the list endpoint returns summaries from the read model.
	rec = do(t, srv, http.MethodGet, "/api/v1/orders?limit=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	var summaries []apporder.SummaryView
	_ = json.Unmarshal(rec.Body.Bytes(), &summaries)
	if len(summaries) != 1 || summaries[0].ID != created.ID || summaries[0].ItemCount != 2 {
		t.Fatalf("list summaries: got %+v", summaries)
	}
}

// TestDeactivatingUserCancelsOpenOrders exercises the CROSS-AGGREGATE reaction:
// deactivating the customer publishes user.deactivated, the subscribed handler
// cancels their open orders. This proves the event-bus wiring end to end.
func TestDeactivatingUserCancelsOpenOrders(t *testing.T) {
	srv := newTestServer()
	uid := registerActiveUser(t, srv, "loyal@example.com")

	// Create + place an order (status "placed", i.e. open).
	rec := do(t, srv, http.MethodPost, "/api/v1/orders", map[string]any{"customer_id": uid, "currency": "EUR"})
	var o apporder.OrderView
	_ = json.Unmarshal(rec.Body.Bytes(), &o)
	_ = do(t, srv, http.MethodPost, "/api/v1/orders/"+o.ID+"/lines",
		map[string]any{"product_id": "SKU-1", "unit_price_minor": 1000, "currency": "EUR", "quantity": 1})
	_ = do(t, srv, http.MethodPost, "/api/v1/orders/"+o.ID+"/place", nil)

	// Deactivate the customer -> event -> handler cancels the order.
	if rec := do(t, srv, http.MethodPost, "/api/v1/users/"+uid+"/deactivate", nil); rec.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", rec.Code, rec.Body.String())
	}

	// The order should now be cancelled.
	rec = do(t, srv, http.MethodGet, "/api/v1/orders/"+o.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get order: %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &o)
	if o.Status != "cancelled" {
		t.Fatalf("cross-aggregate reaction failed: order status = %q, want cancelled", o.Status)
	}
}
