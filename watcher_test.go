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

func TestWatcherGetWatcher(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	app := &App{log: zap.NewNop()}
	app.Start()

	watcher := app.GetWatcher(tmpDir)
	if watcher == nil {
		t.Fatal("GetOrCreateWatcher returned nil")
	}

	if watcher.Root != tmpDir {
		t.Errorf("Watcher.Root = %q, want %q", watcher.Root, tmpDir)
	}

	if watcher.app != app {
		t.Errorf("Watcher.app = %v, want %v", watcher.app, app)
	}

	result := app.GetWatcher(tmpDir)
	if result != watcher {
		t.Errorf("GetOrCreateWatcher returned %v, want %v", result, watcher)
	}

	app.Stop()
}

func TestWatcherIsReady(t *testing.T) {
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

func TestWatcherSubmit(t *testing.T) {
	watcher := &Watcher{
		mutex: sync.Mutex{},
		log:   zap.NewNop(),
	}

	order := &Order{
		Host:     "http://localhost:8080",
		Match:    []string{"*.html", "*.md"},
		Paths:    []string{"/api"},
		CatchAll: []string{"/index.html", "/404.html"},
	}

	oldCmd := &execCmd{}
	watcher.cmd = oldCmd

	newCmd := &execCmd{}
	watcher.newCmd = newCmd

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

func TestWatcherClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher-close-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test app
	app := &App{
		log: zap.NewNop(),
	}
	app.Start()

	// Create a watcher
	watcher := app.GetWatcher(tmpDir)

	if watcher == nil {
		t.Fatal("GetOrCreateWatcher returned nil")
	}

	watcher.cmd = &execCmd{}
	watcher.newCmd = &execCmd{}

	app.Stop()

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
}

func TestWatcherWatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher-watch-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	app := &App{
		log: zap.NewNop(),
	}

	watcher := &Watcher{
		Root:  tmpDir,
		app:   app,
		log:   zap.NewNop(),
		mutex: sync.Mutex{},
	}

	err = watcher.init()
	if err != nil {
		t.Fatalf("init() returned error: %v", err)
	}

	if watcher.watcher == nil {
		t.Fatal("watcher.watcher is nil after init()")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go watcher.watch(ctx)

	substratePath := filepath.Join(tmpDir, "substrate")
	err = os.WriteFile(substratePath, []byte("#!/bin/sh\necho test"), 0755)
	if err != nil {
		t.Fatalf("Failed to create substrate file: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	watcher.mutex.Lock()
	hasNewCmd := watcher.newCmd != nil
	watcher.mutex.Unlock()

	if !hasNewCmd {
		t.Error("Watcher did not start loading after substrate file creation")
	}

	err = os.Remove(substratePath)
	if err != nil {
		t.Fatalf("Failed to remove substrate file: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	watcher.Close()
}

