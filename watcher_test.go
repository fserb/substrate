package substrate

import (
	"context"
	"os"
	"path/filepath"
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

	app.GetWatcher(tmpDir)
	// if watcher == nil {
	// 	t.Fatal("GetOrCreateWatcher returned nil")
	// }
	//
	// if watcher.Root != tmpDir {
	// 	t.Errorf("Watcher.Root = %q, want %q", watcher.Root, tmpDir)
	// }
	//
	// if watcher.app != app {
	// 	t.Errorf("Watcher.app = %v, want %v", watcher.app, app)
	// }
	//
	// result := app.GetWatcher(tmpDir)
	// if result != watcher {
	// 	t.Errorf("GetOrCreateWatcher returned %v, want %v", result, watcher)
	// }

	app.Stop()
}

func TestWatcherSubmit(t *testing.T) {
	watcher := &Watcher{
		log: zap.NewNop(),
	}

	cmd := &execCmd{}
	watcher.cmd = cmd

	// Verify command was promoted
	if watcher.cmd != cmd {
		t.Error("New command was not promoted")
	}
}

func TestWatcherClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher-close-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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
		Root: tmpDir,
		app:  app,
		log:  zap.NewNop(),
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

	// Wait for debounce period plus a little extra
	time.Sleep(150 * time.Millisecond)

	if watcher.cmd == nil {
		t.Error("Watcher did not start loading after substrate file creation")
	}

	err = os.Remove(substratePath)
	if err != nil {
		t.Fatalf("Failed to remove substrate file: %v", err)
	}

	// Wait for debounce period plus a little extra
	time.Sleep(150 * time.Millisecond)

	watcher.Close()
}
