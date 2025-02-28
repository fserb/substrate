package substrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// Mock implementations for testing
type mockHandler struct {
	called bool
	check  func(r *http.Request)
}

func (d *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	d.called = true
	if d.check != nil {
		d.check(r)
	}
	return nil
}

type mockReverseProxy struct {
	host   string
	called bool
}

func (d *mockReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	d.called = true
	return nil
}

func (d *mockReverseProxy) Provision(ctx caddy.Context) error {
	return nil
}

func (d *mockReverseProxy) SetHost(host string) {
	d.host = host
}

// TestServeHTTP tests the ServeHTTP method
func TestServeHTTP(t *testing.T) {
	// Test with a ready watcher and matching path
	t.Run("WithReadyWatcherAndMatch", func(t *testing.T) {
		// Create a test order
		order := &Order{
			Host:  "http://localhost:8080",
			Paths: []string{"/api"},
		}

		// Create a mock proxy
		proxy := &mockReverseProxy{}

		// Create a watcher with the order
		watcher := &Watcher{
			Order: order,
			cmd:   &execCmd{},
		}

		sh := &SubstrateHandler{
			watcher: watcher,
			proxy:   proxy,
			log:     zap.NewNop(),
		}

		// Create a request with a matching path
		req, _ := http.NewRequest("GET", "/api", nil)
		rr := httptest.NewRecorder()
		next := &mockHandler{}

		// Call ServeHTTP
		err := sh.ServeHTTP(rr, req, next)

		// Check results
		if err != nil {
			t.Errorf("ServeHTTP returned error: %v", err)
		}

		if !proxy.called {
			t.Error("Proxy should have been called")
		}

		if proxy.host != "http://localhost:8080" {
			t.Errorf("Proxy host = %q, want %q", proxy.host, "http://localhost:8080")
		}

		if next.called {
			t.Error("Next handler should not have been called")
		}
	})

	// Test with a ready watcher but no match
	t.Run("WithReadyWatcherNoMatch", func(t *testing.T) {
		// Create a test order
		order := &Order{
			Host:  "http://localhost:8080",
			Paths: []string{"/api"},
		}

		// Create a mock proxy
		proxy := &mockReverseProxy{}

		// Create a test filesystem
		testFS := fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("index")},
		}

		// Create a watcher with the order
		watcher := &Watcher{
			Order: order,
			cmd:   &execCmd{},
		}

		sh := &SubstrateHandler{
			watcher: watcher,
			proxy:   proxy,
			log:     zap.NewNop(),
			fs:      testFS,
		}

		// Create a request with a non-matching path
		req, _ := http.NewRequest("GET", "/other", nil)
		ctx := context.WithValue(req.Context(), "root", ".")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()
		next := &mockHandler{}

		// Call ServeHTTP
		err := sh.ServeHTTP(rr, req, next)

		// Check results
		if err != nil {
			t.Errorf("ServeHTTP returned error: %v", err)
		}

		if proxy.called {
			t.Error("Proxy should not have been called")
		}

		if !next.called {
			t.Error("Next handler should have been called")
		}
	})
}
