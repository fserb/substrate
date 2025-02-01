package substrate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestAppServeHTTP(t *testing.T) {
	sh := &SubstrateHandler{}
	sh.log = zaptest.NewLogger(t)
	key := sh.Key()

	app := &App{
		Substrates: map[string]*SubstrateHandler{
			key: sh,
		},
		log: zaptest.NewLogger(t),
	}

	order := Order{Host: "http://example.com", TryFiles: []string{"a", "abc", "ab"}}
	body, _ := json.Marshal(order)
	req := httptest.NewRequest("POST", "/"+key, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	app.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rw.Code)
	}
	if sh.Order == nil {
		t.Fatal("order not updated")
	}

	if sh.Order.Host != order.Host {
		t.Errorf("expected host %s, got %s", order.Host, sh.Order.Host)
	}

}

func TestAppServeHTTP_InvalidMethod(t *testing.T) {
	app := &App{
		Substrates: make(map[string]*SubstrateHandler),
		log:        zaptest.NewLogger(t),
	}
	req := httptest.NewRequest("GET", "/", nil)
	rw := httptest.NewRecorder()
	app.ServeHTTP(rw, req)
	if rw.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rw.Code)
	}
}

func TestAppServeHTTP_InvalidKey(t *testing.T) {
	app := &App{
		Substrates: make(map[string]*SubstrateHandler),
		log:        zaptest.NewLogger(t),
	}
	order := Order{Host: "http://example.com"}
	body, _ := json.Marshal(order)
	req := httptest.NewRequest("POST", "/nonexistent-key", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	app.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rw.Code)
	}
}

func TestAppServeHTTP_InvalidJSON(t *testing.T) {
	sh := &SubstrateHandler{Command: []string{"echo", "test"}}
	sh.log = zaptest.NewLogger(t)
	key := sh.Key()

	app := &App{
		Substrates: map[string]*SubstrateHandler{key: sh},
		log:        zaptest.NewLogger(t),
	}
	req := httptest.NewRequest("POST", "/"+key, bytes.NewReader([]byte("{invalid json")))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	app.ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rw.Code)
	}
}

func TestAppServeHTTP_InvalidContentType(t *testing.T) {
	sh := &SubstrateHandler{Command: []string{"echo", "test"}}
	sh.log = zaptest.NewLogger(t)
	key := sh.Key()

	app := &App{
		Substrates: map[string]*SubstrateHandler{key: sh},
		log:        zaptest.NewLogger(t),
	}
	order := Order{Host: "http://example.com"}
	body, _ := json.Marshal(order)
	req := httptest.NewRequest("POST", "/"+key, bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rw := httptest.NewRecorder()

	app.ServeHTTP(rw, req)
	if rw.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415, got %d", rw.Code)
	}
}

func TestAppServeHTTP_UpdateOrderOverwrite(t *testing.T) {
	// Create a substrate handler with an initial command.
	sh := &SubstrateHandler{Command: []string{"echo", "test"}}
	sh.log = zaptest.NewLogger(t)
	key := sh.Key()

	app := &App{
		Substrates: map[string]*SubstrateHandler{key: sh},
		log:        zaptest.NewLogger(t),
	}

	// First update.
	order1 := Order{
		Host:     "http://example.com",
		TryFiles: []string{"file1", "file2"},
		Match:    []string{"match1"},
	}
	body1, _ := json.Marshal(order1)
	req1 := httptest.NewRequest("POST", "/"+key, bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	rw1 := httptest.NewRecorder()
	app.ServeHTTP(rw1, req1)
	if rw1.Code != http.StatusOK {
		t.Errorf("first update: expected 200, got %d", rw1.Code)
	}
	if sh.Order == nil || sh.Order.Host != order1.Host {
		t.Errorf("first update not applied correctly, got %+v", sh.Order)
	}

	// Second update with completely new order.
	order2 := Order{
		Host:     "http://example.org",
		TryFiles: []string{"newfile"},
		Match:    []string{"newmatch"},
	}
	body2, _ := json.Marshal(order2)
	req2 := httptest.NewRequest("POST", "/"+key, bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rw2 := httptest.NewRecorder()
	app.ServeHTTP(rw2, req2)
	if rw2.Code != http.StatusOK {
		t.Errorf("second update: expected 200, got %d", rw2.Code)
	}
	// Check that the order was overwritten (not merged)
	if sh.Order == nil {
		t.Fatal("second update: order is nil")
	}
	if sh.Order.Host != order2.Host {
		t.Errorf("expected host %q, got %q", order2.Host, sh.Order.Host)
	}
	if len(sh.Order.TryFiles) != len(order2.TryFiles) {
		t.Errorf("expected %d try files, got %d", len(order2.TryFiles), len(sh.Order.TryFiles))
	} else {
		for i, tf := range order2.TryFiles {
			if sh.Order.TryFiles[i] != tf {
				t.Errorf("expected try file %q at index %d, got %q", tf, i, sh.Order.TryFiles[i])
			}
		}
	}
	if len(sh.Order.Match) != len(order2.Match) {
		t.Errorf("expected %d match items, got %d", len(order2.Match), len(sh.Order.Match))
	} else {
		for i, m := range order2.Match {
			if sh.Order.Match[i] != m {
				t.Errorf("expected match %q at index %d, got %q", m, i, sh.Order.Match[i])
			}
		}
	}
}

