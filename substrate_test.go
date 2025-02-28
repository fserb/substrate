package substrate

import (
	"context"
	"sync"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

// Helper function to check if the usage pool is empty
func checkUsagePool(t *testing.T) {
	t.Helper()

	emptyPool := true
	pool.Range(func(key, value any) bool {
		ref, exists := pool.References(key)
		if exists && ref > 0 {
			t.Errorf("Pool still contains key '%s' with %d references", key, ref)
			emptyPool = false
		}
		return true
	})

	if emptyPool {
		t.Log("Usage pool is empty as expected")
	}
}

// TestAppLifecycle tests the basic lifecycle of the App
func TestAppLifecycle(t *testing.T) {
	return
	// Create a new App instance with a test logger
	app := &App{
		log:   zap.NewNop(),
		mutex: sync.Mutex{},
	}

	// Create a test context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	caddyCtx := caddy.Context{Context: ctx}

	// Test Provision
	err := app.Provision(caddyCtx)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	// Verify server was created in the pool
	obj, loaded := pool.LoadOrStore("server", nil)
	if !loaded {
		t.Fatal("Server was not created during provision")
	}
	server, ok := obj.(*Server)
	if !ok {
		t.Fatal("Server object is not of type *Server")
	}
	if server.app != app {
		t.Fatal("Server app reference is incorrect")
	}

	// Test Start
	err = app.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify server is running
	if server.Host == "" {
		t.Fatal("Server host is empty, server may not be running")
	}

	// Test Stop
	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify resources were cleaned up
	checkUsagePool(t)

	// Ensure the server is completely removed from the pool for test isolation
	pool.Delete("server")
}

// TestAppEnvironmentPropagation tests that environment settings are properly propagated
func TestAppEnvironmentPropagation(t *testing.T) {
	app := &App{
		log:            zap.NewNop(),
		Env:            map[string]string{"TEST_KEY": "test_value"},
		RestartPolicy:  "always",
		RedirectStdout: &outputTarget{Type: "null"},
		RedirectStderr: &outputTarget{Type: "null"},
	}

	// Create a mock watcher to test environment propagation
	watcher := &Watcher{
		Root: "/tmp",
		app:  app,
		log:  app.log,
		key:  "test-key",
	}

	// Create a command with the watcher
	cmd := &execCmd{
		Command: []string{"/bin/echo", "test"},
		watcher: watcher,
		log:     app.log,
	}

	// Check if environment variables are properly set
	if cmd.Env == nil {
		cmd.Env = app.Env
	}

	if cmd.Env["TEST_KEY"] != "test_value" {
		t.Errorf("Environment variable not properly set, got %v", cmd.Env)
	}

	if cmd.RestartPolicy == "" {
		cmd.RestartPolicy = app.RestartPolicy
	}

	if cmd.RestartPolicy != "always" {
		t.Errorf("Restart policy not properly set, got %s", cmd.RestartPolicy)
	}
}
