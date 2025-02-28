package substrate

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestGetWatcher tests the GetWatcher function
func TestGetWatcher(t *testing.T) {
	// Create a test watcher and register it
	watcher := &Watcher{
		Root: "/tmp",
		key:  "test-key",
	}

	// Store the watcher in the pool
	watcherPool.LoadOrStore("test-key", watcher)
	defer watcherPool.Delete("test-key")

	// Test getting an existing watcher
	result := GetWatcher("test-key")
	if result != watcher {
		t.Errorf("GetWatcher returned %v, want %v", result, watcher)
	}

	// Test getting a non-existent watcher
	result = GetWatcher("nonexistent")
	if result != nil {
		t.Errorf("GetWatcher returned %v, want nil", result)
	}
}

// TestGetOrCreateWatcher tests the GetOrCreateWatcher function
func TestGetOrCreateWatcher(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "watcher-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test app
	app := &App{
		log: zap.NewNop(),
	}

	// Test creating a new watcher
	watcher := GetOrCreateWatcher(tmpDir, app)
	if watcher == nil {
		t.Fatal("GetOrCreateWatcher returned nil")
	}

	if watcher.Root != tmpDir {
		t.Errorf("Watcher.Root = %q, want %q", watcher.Root, tmpDir)
	}

	if watcher.app != app {
		t.Errorf("Watcher.app = %v, want %v", watcher.app, app)
	}

	// Test getting an existing watcher
	result := GetOrCreateWatcher(tmpDir, app)
	if result != watcher {
		t.Errorf("GetOrCreateWatcher returned %v, want %v", result, watcher)
	}

	// Clean up
	watcher.Close()
}

// TestWatcherIsReady tests the IsReady method
func TestWatcherIsReady(t *testing.T) {
	// Create a test watcher
	watcher := &Watcher{
		mutex: sync.Mutex{},
	}

	// Test with nil cmd and order
	if watcher.IsReady() {
		t.Error("IsReady() = true, want false with nil cmd and order")
	}

	// Test with cmd but nil order
	watcher.cmd = &execCmd{}
	if watcher.IsReady() {
		t.Error("IsReady() = true, want false with nil order")
	}

	// Test with cmd and order
	watcher.Order = &Order{}
	if !watcher.IsReady() {
		t.Error("IsReady() = false, want true with cmd and order")
	}
}

// TestWatcherWaitUntilReady tests the WaitUntilReady method
func TestWatcherWaitUntilReady(t *testing.T) {
	// Create a test watcher
	watcher := &Watcher{
		mutex: sync.Mutex{},
		Root:  t.TempDir(),
	}

	// Test with already ready watcher
	watcher.cmd = &execCmd{}
	watcher.Order = &Order{}
	if !watcher.WaitUntilReady(100 * time.Millisecond) {
		t.Error("WaitUntilReady() = false, want true for ready watcher")
	}

	// Test with no substrate file
	watcher.cmd = nil
	watcher.Order = nil
	if watcher.WaitUntilReady(100 * time.Millisecond) {
		t.Error("WaitUntilReady() = true, want false with no substrate file")
	}

	// Test with substrate file but not ready
	substratePath := filepath.Join(watcher.Root, "substrate")
	err := os.WriteFile(substratePath, []byte("#!/bin/sh\necho test"), 0755)
	if err != nil {
		t.Fatalf("Failed to create substrate file: %v", err)
	}

	// Should timeout waiting for ready
	if watcher.WaitUntilReady(100 * time.Millisecond) {
		t.Error("WaitUntilReady() = true, want false when timeout occurs")
	}

	// Test becoming ready during wait
	go func() {
		time.Sleep(50 * time.Millisecond)
		watcher.mutex.Lock()
		watcher.cmd = &execCmd{}
		watcher.Order = &Order{}
		watcher.mutex.Unlock()
	}()

	if !watcher.WaitUntilReady(200 * time.Millisecond) {
		t.Error("WaitUntilReady() = false, want true when becoming ready during wait")
	}
}

// TestWatcherSubmit tests the Submit method
func TestWatcherSubmit(t *testing.T) {
	// Create a test watcher
	watcher := &Watcher{
		mutex: sync.Mutex{},
		log:   zap.NewNop(),
	}

	// Create a test order
	order := &Order{
		Host:     "http://localhost:8080",
		Match:    []string{"*.html", "*.md"},
		Paths:    []string{"/api"},
		CatchAll: []string{"/index.html", "/404.html"},
	}

	// Create a test command
	oldCmd := &execCmd{}
	watcher.cmd = oldCmd

	// Create a new command
	newCmd := &execCmd{}
	watcher.newCmd = newCmd

	// Submit the order
	watcher.Submit(order)

	// Verify the order was processed
	if watcher.Order != order {
		t.Error("Order was not set correctly")
	}

	// Verify matchers were created
	if len(order.matchers) != 2 {
		t.Errorf("Expected 2 matchers, got %d", len(order.matchers))
	}

	// Verify command was promoted
	if watcher.cmd != newCmd {
		t.Error("New command was not promoted")
	}

	if watcher.newCmd != nil {
		t.Error("newCmd should be nil after promotion")
	}
}

// TestWatcherClose tests the Close method
func TestWatcherClose(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "watcher-close-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test app
	app := &App{
		log: zap.NewNop(),
	}

	// Create a watcher
	watcher := GetOrCreateWatcher(tmpDir, app)
	if watcher == nil {
		t.Fatal("GetOrCreateWatcher returned nil")
	}

	// Store the key for later verification
	key := watcher.key

	// Create test commands
	watcher.cmd = &execCmd{}
	watcher.newCmd = &execCmd{}

	// Close the watcher
	err = watcher.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Verify resources were cleaned up
	if watcher.cancel != nil {
		t.Error("cancel function was not cleared")
	}

	if watcher.watcher != nil {
		t.Error("fsnotify watcher was not cleared")
	}

	if watcher.cmd != nil {
		t.Error("cmd was not cleared")
	}

	if watcher.newCmd != nil {
		t.Error("newCmd was not cleared")
	}

	// Verify watcher was removed from pool
	if obj, loaded := watcherPool.LoadOrStore(key, nil); loaded {
		t.Errorf("Watcher was not removed from pool, got %v", obj)
	}
}

// TestWatcherWatch tests the watch method
func TestWatcherWatch(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "watcher-watch-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test app
	app := &App{
		log: zap.NewNop(),
	}

	// Create a watcher
	watcher := &Watcher{
		Root:  tmpDir,
		app:   app,
		log:   zap.NewNop(),
		mutex: sync.Mutex{},
	}

	// Initialize the watcher
	err = watcher.init()
	if err != nil {
		t.Fatalf("init() returned error: %v", err)
	}

	// Create a context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watching in a goroutine
	go watcher.watch(ctx)

	// Create a substrate file
	substratePath := filepath.Join(tmpDir, "substrate")
	err = os.WriteFile(substratePath, []byte("#!/bin/sh\necho test"), 0755)
	if err != nil {
		t.Fatalf("Failed to create substrate file: %v", err)
	}

	// Wait for the watcher to detect the file
	time.Sleep(100 * time.Millisecond)

	// Verify the watcher started loading
	watcher.mutex.Lock()
	hasNewCmd := watcher.newCmd != nil
	watcher.mutex.Unlock()

	if !hasNewCmd {
		t.Error("Watcher did not start loading after substrate file creation")
	}

	// Remove the substrate file
	err = os.Remove(substratePath)
	if err != nil {
		t.Fatalf("Failed to remove substrate file: %v", err)
	}

	// Wait for the watcher to detect the removal
	time.Sleep(100 * time.Millisecond)

	// Clean up
	watcher.Close()
}
