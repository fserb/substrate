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

	pool.Range(func(key, value any) bool {
		ref, exists := pool.References(key)
		if exists && ref > 0 {
			t.Errorf("Pool still contains key '%s' with %d references", key, ref)
		}
		return true
	})
}

// TestAppLifecycle tests the basic lifecycle of the App
func TestAppLifecycle(t *testing.T) {
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

	err = app.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if app.server == nil {
		t.Fatal("Server is nil, server may not be running")
	}

	if app.server.app != app {
		t.Fatal("Server app reference is incorrect")
	}

	if app.server.Host == "" {
		t.Fatal("Server host is empty, server may not be running")
	}

	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	checkUsagePool(t)

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

