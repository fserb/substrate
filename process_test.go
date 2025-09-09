package substrate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

func TestProcessManager_GetOrCreateHost(t *testing.T) {
	// Create a test logger
	logger := zap.NewNop()

	// Create process manager config with shorter timeouts for testing
	config := ProcessManagerConfig{
		IdleTimeout:    caddy.Duration(time.Minute),
		StartupTimeout: caddy.Duration(100 * time.Millisecond), // Much shorter for tests
		Logger:         logger,
	}

	// Create process manager
	pm, err := NewProcessManager(config)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()


	// Test getOrCreateHost with a simple command that exits quickly
	filePath := "/bin/echo"
	hostPort, err := pm.getOrCreateHost(filePath)
	if err != nil {
		t.Fatalf("Failed to get host:port: %v", err)
	}

	// Verify we got a valid host:port
	if hostPort == "" {
		t.Error("Host:port should not be empty")
	}

	// Should be in format "localhost:port"
	if len(hostPort) < 10 { // "localhost:" is 10 chars, plus at least 1 digit
		t.Errorf("Host:port format looks incorrect: %s", hostPort)
	}
}

func TestProcessManager_MultipleProcesses(t *testing.T) {
	logger := zap.NewNop()
	config := ProcessManagerConfig{
		IdleTimeout:    caddy.Duration(time.Minute),
		StartupTimeout: caddy.Duration(100 * time.Millisecond), // Shorter for tests
		Logger:         logger,
	}

	pm, err := NewProcessManager(config)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Test multiple calls with different files - use actual file paths
	files := []string{"/bin/echo", "/bin/sleep", "/bin/cat"}
	hostPorts := make([]string, 0, len(files))

	for _, file := range files {
		hostPort, err := pm.getOrCreateHost(file)
		if err != nil {
			t.Fatalf("Failed to get host:port for %s: %v", file, err)
		}
		hostPorts = append(hostPorts, hostPort)
	}

	// Verify all processes got different host:ports
	for i, hostPort := range hostPorts {
		if hostPort == "" {
			t.Errorf("Host:port %d should not be empty", i)
		}
		// Each should be unique (different ports)
		for j, otherHostPort := range hostPorts {
			if i != j && hostPort == otherHostPort {
				t.Errorf("Host:ports should be unique, but %s == %s", hostPort, otherHostPort)
			}
		}
	}
}

func TestProcessManager_DifferentFiles(t *testing.T) {
	logger := zap.NewNop()
	config := ProcessManagerConfig{
		IdleTimeout:    caddy.Duration(time.Minute),
		StartupTimeout: caddy.Duration(100 * time.Millisecond), // Shorter for tests
		Logger:         logger,
	}

	pm, err := NewProcessManager(config)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Test that calling getOrCreateHost twice for same file 
	// Since sleep exits immediately with no args, we'll test the creation behavior
	file := "/bin/sleep"

	// First call
	hostPort1, err := pm.getOrCreateHost(file)
	if err != nil {
		t.Fatalf("Failed to get host:port first time: %v", err)
	}

	// Verify we got a valid host:port
	if hostPort1 == "" {
		t.Error("First host:port should not be empty")
	}

	// Second call for different file should get different port
	file2 := "/bin/echo"
	hostPort2, err := pm.getOrCreateHost(file2)
	if err != nil {
		t.Fatalf("Failed to get host:port for second file: %v", err)
	}

	// Should be different ports for different files
	if hostPort1 == hostPort2 {
		t.Errorf("Different files should get different host:ports, but both got %s", hostPort1)
	}
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
		// For sleep processes, termination signals are expected
		t.Logf("Process stop returned error (expected for SIGTERM): %v", err)
	}

	// Verify it's stopped
	if process.IsRunning() {
		t.Error("Process should be stopped after Stop()")
	}
}

func TestValidateFilePath(t *testing.T) {
	// Create a temporary directory and file for testing
	tmpDir := t.TempDir()
	validFile := filepath.Join(tmpDir, "test.sh")
	err := os.WriteFile(validFile, []byte("#!/bin/bash\necho hello"), 0755)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test valid absolute path
	err = validateFilePath(validFile)
	if err != nil {
		t.Errorf("Valid absolute path should pass validation: %v", err)
	}

	// Test non-existent file
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.sh")
	err = validateFilePath(nonExistentFile)
	if err == nil {
		t.Error("Non-existent file should fail validation")
	}

	// Test relative path
	err = validateFilePath("relative/path.sh")
	if err == nil {
		t.Error("Relative path should fail validation")
	}

	// Test path traversal
	traversalPath := filepath.Join(tmpDir, "../../../etc/passwd")
	err = validateFilePath(traversalPath)
	if err == nil {
		t.Error("Path traversal should fail validation")
	}

	// Test directory instead of file
	err = validateFilePath(tmpDir)
	if err == nil {
		t.Error("Directory should fail validation")
	}
}

func TestProcessManager_GetOrCreateHost_FileValidation(t *testing.T) {
	logger := zap.NewNop()
	config := ProcessManagerConfig{
		IdleTimeout:    caddy.Duration(time.Minute),
		StartupTimeout: caddy.Duration(100 * time.Millisecond),
		Logger:         logger,
	}

	pm, err := NewProcessManager(config)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Test with non-existent file
	_, err = pm.getOrCreateHost("/nonexistent/file.sh")
	if err == nil {
		t.Error("getOrCreateHost should fail for non-existent file")
	}

	// Test with relative path
	_, err = pm.getOrCreateHost("relative/path.sh")
	if err == nil {
		t.Error("getOrCreateHost should fail for relative path")
	}

	// Test with directory
	tmpDir := t.TempDir()
	_, err = pm.getOrCreateHost(tmpDir)
	if err == nil {
		t.Error("getOrCreateHost should fail for directory")
	}
}

