package substrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap/zaptest"
)

func TestProcessManager_ProcessExitCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	deno := NewDenoManager(logger)
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),   // idle timeout
		caddy.Duration(1*time.Second), // startup timeout
		nil,                           // no env vars for this test
		deno,
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	tmpDir := t.TempDir()
	exitScript := filepath.Join(tmpDir, "exit.js")
	scriptContent := `const server = Deno.serve({
  path: Deno.args[0],
}, () => new Response("OK"));

console.log("Server started, will exit with code 42 after 1 second");

setTimeout(() => {
  server.shutdown();
  Deno.exit(42);
}, 1000);
`
	err = os.WriteFile(exitScript, []byte(scriptContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create exit script: %v", err)
	}

	// Get socket path for the script - this will start the process
	socketPath, err := pm.getOrCreateHost(exitScript)
	if err != nil {
		t.Fatalf("Failed to get socket path: %v", err)
	}

	// Verify the process was added to the map
	pm.mu.RLock()
	_, exists := pm.processes[exitScript]
	pm.mu.RUnlock()

	if !exists {
		t.Error("Process should exist in processes map")
	}

	// Wait for the process to exit and be cleaned up (with timeout)
	maxWait := 3 * time.Second
	checkInterval := 10 * time.Millisecond
	start := time.Now()

	for time.Since(start) < maxWait {
		pm.mu.RLock()
		_, stillExists := pm.processes[exitScript]
		pm.mu.RUnlock()

		if !stillExists {
			break // Process was cleaned up
		}
		time.Sleep(checkInterval)
	}

	// Final verification that the process was removed
	pm.mu.RLock()
	_, stillExists := pm.processes[exitScript]
	pm.mu.RUnlock()

	if stillExists {
		t.Error("Exited process should be removed from processes map")
	}

	// Verify we got a valid socket path initially
	if socketPath == "" {
		t.Error("Socket path should not be empty")
	}
}

func TestProcessManager_NormalExitCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	deno := NewDenoManager(logger)
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),   // idle timeout
		caddy.Duration(3*time.Second), // startup timeout
		nil,                           // no env vars for this test
		deno,
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Create a Deno script that starts a server but exits normally after a short delay
	tmpDir := t.TempDir()
	normalScript := filepath.Join(tmpDir, "normal.js")
	scriptContent := `const [socketPath] = Deno.args;

// Start server
const server = Deno.serve({
  path: socketPath,
}, () => new Response("OK"));

console.log("Server started, will exit normally after 1 second");

// Exit normally after delay - delay long enough for startup timeout to complete
setTimeout(() => {
  server.shutdown();
  Deno.exit(0);
}, 1000);
`
	err = os.WriteFile(normalScript, []byte(scriptContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create normal exit script: %v", err)
	}

	// Get socket path for the script - this will start the process
	socketPath, err := pm.getOrCreateHost(normalScript)
	if err != nil {
		t.Fatalf("Failed to get socket path: %v", err)
	}

	// Verify the process was added to the map
	pm.mu.RLock()
	_, exists := pm.processes[normalScript]
	pm.mu.RUnlock()

	if !exists {
		t.Error("Process should exist in processes map")
	}

	// Wait for the process to exit normally and be cleaned up
	maxWait := 3 * time.Second
	checkInterval := 10 * time.Millisecond
	start := time.Now()

	for time.Since(start) < maxWait {
		pm.mu.RLock()
		_, stillExists := pm.processes[normalScript]
		pm.mu.RUnlock()

		if !stillExists {
			break // Process was cleaned up
		}
		time.Sleep(checkInterval)
	}

	// Verify even normally exited processes are removed from the map
	pm.mu.RLock()
	_, stillExists := pm.processes[normalScript]
	pm.mu.RUnlock()

	if stillExists {
		t.Error("Normally exited process should also be removed from processes map")
	}

	// Verify we got a valid socket path initially
	if socketPath == "" {
		t.Error("Socket path should not be empty")
	}
}

func TestValidateFilePath(t *testing.T) {
	// Create a temporary directory and file for testing
	tmpDir := t.TempDir()
	validFile := filepath.Join(tmpDir, "test.js")
	err := os.WriteFile(validFile, []byte("console.log('hello')"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test valid absolute path
	err = validateFilePath(validFile)
	if err != nil {
		t.Errorf("Valid absolute path should pass validation: %v", err)
	}

	// Test non-existent file
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.js")
	err = validateFilePath(nonExistentFile)
	if err == nil {
		t.Error("Non-existent file should fail validation")
	}

	// Test relative path
	err = validateFilePath("relative/path.js")
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
	logger := zaptest.NewLogger(t)
	deno := NewDenoManager(logger)
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),   // idle timeout
		caddy.Duration(3*time.Second), // startup timeout
		nil,                           // no env vars for this test
		deno,
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Test with non-existent file
	_, err = pm.getOrCreateHost("/nonexistent/file.js")
	if err == nil {
		t.Error("getOrCreateHost should fail for non-existent file")
	}

	// Test with relative path
	_, err = pm.getOrCreateHost("relative/path.js")
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

func TestProcess_CrashDetection(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a process that will crash (exit with code 1)
	process := &Process{
		ScriptPath: "sh",
		SocketPath: "/tmp/test.sock",
		LastUsed:   time.Now(),
		onExit:     func() {},
		logger:     logger,
		exitChan:   make(chan struct{}),
	}

	// Override the command args to make it crash
	process.mu.Lock()
	// We need to manually set this up since start() would construct normal socket path args
	process.Cmd = exec.Command("sh", "-c", "exit 1")
	process.mu.Unlock()

	// Start the command directly
	if err := process.Cmd.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Start monitoring
	go process.monitor()

	// Wait a moment for the process to exit
	time.Sleep(100 * time.Millisecond)

	// Check the exit code was captured
	process.mu.RLock()
	exitCode := process.exitCode
	process.mu.RUnlock()

	if exitCode != 1 {
		t.Errorf("Expected exit code 1 (crash), got %d", exitCode)
	}
}

func TestValidateFilePath_Symlink(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a valid script file
	realFile := filepath.Join(tmpDir, "test.js")
	err := os.WriteFile(realFile, []byte("console.log('hello')"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a symlink to the script file
	symlinkPath := filepath.Join(tmpDir, "test_symlink.js")
	err = os.Symlink(realFile, symlinkPath)
	if err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Test that symlink to valid file passes validation
	err = validateFilePath(symlinkPath)
	if err != nil {
		t.Errorf("Symlink to file should pass validation: %v", err)
	}

	// Create a broken symlink
	brokenSymlink := filepath.Join(tmpDir, "broken_symlink.js")
	err = os.Symlink("/nonexistent/target", brokenSymlink)
	if err != nil {
		t.Fatalf("Failed to create broken symlink: %v", err)
	}

	// Test that broken symlink fails validation
	err = validateFilePath(brokenSymlink)
	if err == nil {
		t.Error("Broken symlink should fail validation")
	}

	// Create a symlink to a text file
	textFile := filepath.Join(tmpDir, "content.txt")
	err = os.WriteFile(textFile, []byte("content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create text file: %v", err)
	}

	textSymlink := filepath.Join(tmpDir, "content_symlink.txt")
	err = os.Symlink(textFile, textSymlink)
	if err != nil {
		t.Fatalf("Failed to create symlink to text file: %v", err)
	}

	// Test that symlink to any regular file passes validateFilePath
	// (Deno handles execution, not the OS)
	err = validateFilePath(textSymlink)
	if err != nil {
		t.Errorf("Symlink to text file should pass validateFilePath: %v", err)
	}
}
