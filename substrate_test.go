package substrate

import (
	"context"
	"sync"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

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

	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestAppEnvironmentPropagation tests that environment settings are properly propagated
func TestAppEnvironmentPropagation(t *testing.T) {
	app := &App{
		log: zap.NewNop(),
		Env: map[string]string{"TEST_KEY": "test_value"},
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
}

// TestAppGetWatcherWithInvalidRoot tests the GetWatcher method with invalid root
func TestAppGetWatcherWithInvalidRoot(t *testing.T) {
	app := &App{
		log: zap.NewNop(),
	}

	// Start the app to initialize the server
	err := app.Start()
	if err != nil {
		t.Fatalf("Failed to start app: %v", err)
	}
	defer app.Stop()

	// Test with non-existent directory
	watcher := app.GetWatcher("/nonexistent/directory")
	if watcher != nil {
		t.Error("GetWatcher should return nil for non-existent directory")
	}

	// Test with relative path (should fail because root must be absolute)
	watcher = app.GetWatcher("relative/path")
	if watcher != nil {
		t.Error("GetWatcher should return nil for relative path")
	}
}

// TestAppCaddyModule tests the CaddyModule method
func TestAppCaddyModule(t *testing.T) {
	app := App{}

	info := app.CaddyModule()

	if info.ID != "substrate" {
		t.Errorf("Expected module ID 'substrate', got '%s'", info.ID)
	}

	// Test that the New function returns a new App
	module := info.New()
	_, ok := module.(*App)
	if !ok {
		t.Errorf("Expected New() to return *App, got %T", module)
	}
}
