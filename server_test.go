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

// TestServerStart tests the Start method of the Server
func TestServerStart(t *testing.T) {
	server := &Server{
		log: zap.NewNop(),
	}

	// Start the server
	err := server.Start()
	if err != nil {
		t.Fatalf("Server.Start() failed: %v", err)
	}

	// Verify the server is running
	if server.Host == "" {
		t.Fatal("Server.Host is empty, server may not be running")
	}

	if !strings.HasPrefix(server.Host, "http://localhost:") {
		t.Errorf("Server.Host = %q, want prefix 'http://localhost:'", server.Host)
	}

	// Verify readyCh is closed
	select {
	case <-server.readyCh:
		// Channel is closed as expected
	default:
		t.Error("server.readyCh is not closed")
	}

	// Clean up
	server.Stop()
}

// TestServerWaitForStart tests the WaitForStart method
func TestServerWaitForStart(t *testing.T) {
	server := &Server{
		log: zap.NewNop(),
	}

	// Start the server
	err := server.Start()
	if err != nil {
		t.Fatalf("Server.Start() failed: %v", err)
	}

	// Create a test app
	app := &App{
		log: zap.NewNop(),
	}

	// Create a channel to signal when WaitForStart returns
	done := make(chan struct{})

	// Call WaitForStart in a goroutine
	go func() {
		server.WaitForStart(app)
		close(done)
	}()

	// Wait for WaitForStart to return or timeout
	select {
	case <-done:
		// WaitForStart returned as expected
	case <-time.After(1 * time.Second):
		t.Fatal("WaitForStart did not return in time")
	}

	// Verify app was set
	if server.app != app {
		t.Error("server.app was not set correctly")
	}

	// Clean up
	server.Stop()
}

// TestServerStop tests the Stop method
func TestServerStop(t *testing.T) {
	server := &Server{
		log: zap.NewNop(),
	}

	// Start the server
	err := server.Start()
	if err != nil {
		t.Fatalf("Server.Start() failed: %v", err)
	}

	// Store the host
	host := server.Host

	// Stop the server
	server.Stop()

	// Verify the server is stopped
	if server.Host != "" {
		t.Errorf("Server.Host = %q, want empty string", server.Host)
	}

	if server.readyCh != nil {
		t.Error("server.readyCh should be nil")
	}

	if server.app != nil {
		t.Error("server.app should be nil")
	}

	// Try to connect to the server (should fail)
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}
	_, err = client.Get(host)
	if err == nil {
		t.Error("Expected error when connecting to stopped server")
	}
}

// TestServerServeHTTP tests the ServeHTTP method
func TestServerServeHTTP(t *testing.T) {
	// Test with nil app
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

	// Test with invalid method
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

	// Test with invalid content type
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

	// Test with valid request but nonexistent watcher
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

	// Test reset endpoint
	// 	t.Run("ResetEndpoint", func(t *testing.T) {
	// 		server := &Server{
	// 			log: zap.NewNop(),
	// 			app: &App{},
	// 		}
	//
	// 		req := httptest.NewRequest("GET", "/reset", nil)
	// 		rr := httptest.NewRecorder()
	//
	// 		// Create a variable to track if clearCache would be called
	// 		clearCacheCalled := false
	//
	// 		// Save the original function
	// 		originalFunc := clearCache
	//
	// 		// Replace with a test version
	// 		clearCacheTest := func() error {
	// 			clearCacheCalled = true
	// 			return nil
	// 		}
	//
	// 		// Use the test version for this test
	// 		clearCache = clearCacheTest
	// 		defer func() { clearCache = originalFunc }()
	//
	// 		server.ServeHTTP(rr, req)
	//
	// 		if !clearCacheCalled {
	// 			t.Error("clearCache was not called")
	// 		}
	//
	// 		if rr.Code != http.StatusOK {
	// 			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	// 		}
	// 	})
}

// TestServerSubmitOrder tests submitting an order to a watcher
func TestServerSubmitOrder(t *testing.T) {
	// Create a test watcher and register it
	watcher := &Watcher{
		Root: "/tmp",
		key:  "test-key",
		log:  zap.NewNop(),
		cmd:  &execCmd{},
	}

	// Store the watcher in the pool
	watcherPool.LoadOrStore("test-key", watcher)
	defer watcherPool.Delete("test-key")

	// Create a server
	server := &Server{
		log: zap.NewNop(),
		app: &App{},
	}

	// Create a test order
	order := Order{
		Host:  "http://localhost:8080",
		Paths: []string{"/api"},
	}

	// Create a request with the order
	orderJSON, _ := json.Marshal(order)
	req := httptest.NewRequest("POST", "/test-key", strings.NewReader(string(orderJSON)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	// Call ServeHTTP
	server.ServeHTTP(rr, req)

	// Check response
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Verify the order was submitted to the watcher
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
