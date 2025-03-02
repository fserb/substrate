package substrate

import (
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestGetDebugInfo(t *testing.T) {
	// Create a test server with some watchers
	server := &Server{
		log:      zap.NewNop(),
		watchers: make(map[string]*Watcher),
	}

	// Add a ready watcher
	readyWatcher := &Watcher{
		Root:  "/tmp/ready",
		Order: &Order{},
		cmd:   &execCmd{},
	}
	server.watchers["ready"] = readyWatcher

	// Add a not-ready watcher
	notReadyWatcher := &Watcher{
		Root: "/tmp/not-ready",
	}
	server.watchers["not-ready"] = notReadyWatcher

	// Get debug info
	info := GetDebugInfo(server)

	// Verify basic info
	if info.GoVersion != runtime.Version() {
		t.Errorf("Expected GoVersion %s, got %s", runtime.Version(), info.GoVersion)
	}

	if info.GOOS != runtime.GOOS {
		t.Errorf("Expected GOOS %s, got %s", runtime.GOOS, info.GOOS)
	}

	if info.GOARCH != runtime.GOARCH {
		t.Errorf("Expected GOARCH %s, got %s", runtime.GOARCH, info.GOARCH)
	}

	if info.NumCPU != runtime.NumCPU() {
		t.Errorf("Expected NumCPU %d, got %d", runtime.NumCPU(), info.NumCPU)
	}

	// Verify watchers info
	if len(info.Watchers) != 2 {
		t.Errorf("Expected 2 watchers, got %d", len(info.Watchers))
	}

	if info.Watchers["ready"] != "/tmp/ready" {
		t.Errorf("Expected ready watcher path /tmp/ready, got %s", info.Watchers["ready"])
	}

	if info.Watchers["not-ready"] != "/tmp/not-ready (not ready)" {
		t.Errorf("Expected not-ready watcher path '/tmp/not-ready (not ready)', got %s", info.Watchers["not-ready"])
	}

	// Test JSON serialization
	_, err := json.Marshal(info)
	if err != nil {
		t.Errorf("Failed to marshal debug info to JSON: %v", err)
	}
}

func TestDebugInfoStartTime(t *testing.T) {
	// Verify that the start time is set correctly
	now := time.Now()

	// Get debug info
	info := GetDebugInfo(nil)

	// Start time should be before now
	if info.StartTime.After(now) {
		t.Errorf("StartTime %v is after current time %v", info.StartTime, now)
	}

	// Uptime should be a positive duration
	uptime, err := time.ParseDuration(info.Uptime)
	if err != nil {
		t.Errorf("Failed to parse uptime %s: %v", info.Uptime, err)
	}

	if uptime < 0 {
		t.Errorf("Expected positive uptime, got %v", uptime)
	}
}
