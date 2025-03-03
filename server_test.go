package substrate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestServerStart(t *testing.T) {
	server := &Server{
		log: zap.NewNop(),
	}

	err := server.Start()
	if err != nil {
		t.Fatalf("Server.Start() failed: %v", err)
	}

	if server.Host == "" {
		t.Fatal("Server.Host is empty, server may not be running")
	}

	if !strings.HasPrefix(server.Host, "http://localhost:") {
		t.Errorf("Server.Host = %q, want prefix 'http://localhost:'", server.Host)
	}

	select {
	case <-server.readyCh:
		// Channel is closed as expected
	default:
		t.Error("server.readyCh is not closed")
	}

	server.Stop()
}

func TestServerWaitForStart(t *testing.T) {
	server := &Server{
		log: zap.NewNop(),
	}

	err := server.Start()
	if err != nil {
		t.Fatalf("Server.Start() failed: %v", err)
	}

	app := &App{
		log: zap.NewNop(),
	}

	done := make(chan struct{})

	go func() {
		server.WaitForStart(app)
		close(done)
	}()

	select {
	case <-done:
		// WaitForStart returned as expected
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForStart did not return in time")
	}

	if server.app != app {
		t.Error("server.app was not set correctly")
	}

	server.Stop()
}

func TestServerStop(t *testing.T) {
	server := &Server{
		log: zap.NewNop(),
	}

	err := server.Start()
	if err != nil {
		t.Fatalf("Server.Start() failed: %v", err)
	}

	host := server.Host

	server.Stop()

	if server.Host != "" {
		t.Errorf("Server.Host = %q, want empty string", server.Host)
	}

	if server.readyCh != nil {
		t.Error("server.readyCh should be nil")
	}

	if server.app != nil {
		t.Error("server.app should be nil")
	}

	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}
	_, err = client.Get(host)
	if err == nil {
		t.Error("Expected error when connecting to stopped server")
	}
}

func TestServerServeHTTP(t *testing.T) {
	t.Run("NilApp", func(t *testing.T) {
		server := &Server{
			log: zap.NewNop(),
		}

		req := httptest.NewRequest("POST", "/test", nil)
		rr := httptest.NewRecorder()

		server.ServeHTTP(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
		}
	})

	t.Run("InvalidMethod", func(t *testing.T) {
		server := &Server{
			log: zap.NewNop(),
			app: &App{},
		}

		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()

		server.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
		}
	})

	t.Run("InvalidContentType", func(t *testing.T) {
		server := &Server{
			log: zap.NewNop(),
			app: &App{},
		}

		req := httptest.NewRequest("POST", "/test", nil)
		req.Header.Set("Content-Type", "text/plain")
		rr := httptest.NewRecorder()

		server.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnsupportedMediaType {
			t.Errorf("Expected status %d, got %d", http.StatusUnsupportedMediaType, rr.Code)
		}
	})

	t.Run("NonexistentWatcher", func(t *testing.T) {
		server := &Server{
			log: zap.NewNop(),
			app: &App{},
		}

		req := httptest.NewRequest("POST", "/nonexistent", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		server.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, rr.Code)
		}
	})

	t.Run("StatusEndpoint", func(t *testing.T) {
		server := &Server{
			log:      zap.NewNop(),
			app:      &App{},
			watchers: make(map[string]*Watcher),
		}

		req := httptest.NewRequest("GET", "/status", nil)
		rr := httptest.NewRecorder()

		server.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}

		if rr.Header().Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type %s, got %s", "application/json", rr.Header().Get("Content-Type"))
		}

		var info DebugInfo
		err := json.Unmarshal(rr.Body.Bytes(), &info)
		if err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}

		if info.GoVersion == "" {
			t.Error("GoVersion is empty")
		}
	})

	t.Run("ResetEndpoint", func(t *testing.T) {
		server := &Server{
			log: zap.NewNop(),
			app: &App{},
		}

		req := httptest.NewRequest("GET", "/reset", nil)
		rr := httptest.NewRecorder()

		server.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}

		if string(rr.Body.Bytes()) != "Cache cleared" {
			t.Errorf("Expected body %q, got %q", "Cache cleared", string(rr.Body.Bytes()))
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		server := &Server{
			log:      zap.NewNop(),
			app:      &App{},
			watchers: make(map[string]*Watcher),
		}

		// Add a test watcher
		watcher := &Watcher{
			Root: "/tmp",
			cmd:  &execCmd{},
			log:  zap.NewNop(),
		}
		server.watchers["test"] = watcher

		req := httptest.NewRequest("POST", "/test", strings.NewReader("invalid json"))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		server.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
		}
	})
}

func TestServerSubmitOrder(t *testing.T) {
	watcher := &Watcher{
		Root: "/tmp",
		log:  zap.NewNop(),
		cmd:  &execCmd{},
	}

	watchers := make(map[string]*Watcher)
	watchers["abc"] = watcher

	server := &Server{
		log:      zap.NewNop(),
		app:      &App{},
		watchers: watchers,
	}

	order := Order{
		Host:  "http://localhost:8080",
		Paths: []string{"/api"},
	}

	orderJSON, _ := json.Marshal(order)
	req := httptest.NewRequest("POST", "/abc", strings.NewReader(string(orderJSON)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if watcher.Order == nil {
		t.Fatal("Watcher order is nil")
	}

	if watcher.Order.Host != "http://localhost:8080" {
		t.Errorf("Expected host %q, got %q", "http://localhost:8080", watcher.Order.Host)
	}

	if len(watcher.Order.Paths) != 1 || watcher.Order.Paths[0] != "/api" {
		t.Errorf("Expected paths [/api], got %v", watcher.Order.Paths)
	}
}
