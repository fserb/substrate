package substrate

import (
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

func TestProcessManager_StartAndStopProcess(t *testing.T) {
	// Create a test logger
	logger := zap.NewNop()

	// Create process manager config
	config := ProcessManagerConfig{
		IdleTimeout:    caddy.Duration(time.Minute),
		StartupTimeout: caddy.Duration(30 * time.Second),
		Logger:         logger,
	}

	// Create process manager
	pm, err := NewProcessManager(config)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Create a simple process config (using echo command which should be available)
	processConfig := ProcessConfig{
		Command: "echo",
		Args:    []string{"hello"},
	}

	// Start the process
	key := "test-process"
	process, err := pm.StartProcess(key, processConfig)
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Verify process is running initially
	if !process.IsRunning() {
		t.Error("Process should be running after start")
	}

	// Wait a bit for the echo command to complete (it exits immediately)
	time.Sleep(100 * time.Millisecond)

	// Echo command should have completed by now
	if process.IsRunning() {
		t.Error("Echo process should have completed")
	}

	// Verify the process key is correct
	if process.Key != key {
		t.Errorf("Expected process key %s, got %s", key, process.Key)
	}
}

func TestProcessManager_MultipleProcesses(t *testing.T) {
	logger := zap.NewNop()
	config := ProcessManagerConfig{
		IdleTimeout:    caddy.Duration(time.Minute),
		StartupTimeout: caddy.Duration(30 * time.Second),
		Logger:         logger,
	}

	pm, err := NewProcessManager(config)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Start multiple processes
	keys := []string{"process1", "process2", "process3"}
	processes := make([]*ManagedProcess, 0, len(keys))

	for _, key := range keys {
		processConfig := ProcessConfig{
			Command: "echo",
			Args:    []string{key},
		}

		process, err := pm.StartProcess(key, processConfig)
		if err != nil {
			t.Fatalf("Failed to start process %s: %v", key, err)
		}
		processes = append(processes, process)
	}

	// Verify all processes were created with correct keys
	for i, process := range processes {
		if process.Key != keys[i] {
			t.Errorf("Expected process key %s, got %s", keys[i], process.Key)
		}
	}
}

func TestProcessManager_UpdateLastUsed(t *testing.T) {
	logger := zap.NewNop()
	config := ProcessManagerConfig{
		IdleTimeout:    caddy.Duration(time.Minute),
		StartupTimeout: caddy.Duration(30 * time.Second),
		Logger:         logger,
	}

	pm, err := NewProcessManager(config)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Start a process
	key := "test-process"
	processConfig := ProcessConfig{
		Command: "sleep",
		Args:    []string{"10"}, // Sleep for 10 seconds
	}

	process, err := pm.StartProcess(key, processConfig)
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Record initial last used time
	initialTime := process.LastUsed

	// Wait a bit and then update last used
	time.Sleep(10 * time.Millisecond)
	pm.UpdateLastUsed(key)

	// Verify last used time was updated
	if !process.LastUsed.After(initialTime) {
		t.Error("Last used time should have been updated")
	}

	// Stop the process
	process.Stop()
}

func TestManagedProcess_Stop(t *testing.T) {
	logger := zap.NewNop()

	// Create a managed process
	process := &ManagedProcess{
		Key:      "test-stop",
		Config:   ProcessConfig{Command: "sleep", Args: []string{"10"}},
		LastUsed: time.Now(),
		running:  false,
		logger:   logger,
	}

	// Start the process
	err := process.start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Verify it's running
	if !process.IsRunning() {
		t.Error("Process should be running after start")
	}

	// Stop the process
	err = process.Stop()
	if err != nil {
		// Check if it's an expected termination signal
		if !isProcessAlreadyFinished(err) {
			t.Fatalf("Failed to stop process: %v", err)
		}
	}

	// Verify it's stopped
	if process.IsRunning() {
		t.Error("Process should be stopped after Stop()")
	}
}



