package substrate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

const simpleServerScript = `#!/usr/bin/env -S deno run --allow-net
// Simple HTTP server for testing substrate transport

const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: simple_server.js <host> <port>");
  Deno.exit(1);
}

const server = Deno.serve({ 
  hostname: host, 
  port: parseInt(port) 
}, (req) => {
  return new Response(` + "`Hello from substrate process!\nHost: ${host}\nPort: ${port}\nURL: ${req.url}\nMethod: ${req.method}\nUser-Agent: ${req.headers.get(\"user-agent\") ?? \"unknown\"}`" + `, {
    headers: { "Content-Type": "text/plain" }
  });
});

console.log(` + "`Server running at http://${host}:${port}/`" + `);

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Received SIGTERM, shutting down gracefully");
  server.shutdown();
  Deno.exit(0);
});`

func TestProcessManager_MultipleProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zap.NewNop()
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),
		caddy.Duration(3*time.Second), // Longer timeout for actual servers
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Test basic getOrCreateHost functionality first
	tmpDir := t.TempDir()
	testScript := filepath.Join(tmpDir, "test_server.js")
	err = os.WriteFile(testScript, []byte(simpleServerScript), 0755)
	if err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	hostPort, err := pm.getOrCreateHost(testScript)
	if err != nil {
		t.Fatalf("Failed to get host:port: %v", err)
	}

	if hostPort == "" {
		t.Error("Host:port should not be empty")
	}
	if len(hostPort) < 10 {
		t.Errorf("Host:port format looks incorrect: %s", hostPort)
	}

	// Create multiple copies of the same script to test unique ports
	files := make([]string, 3)

	// Create multiple script copies with different names
	for i := 0; i < 3; i++ {
		scriptPath := filepath.Join(tmpDir, fmt.Sprintf("server_%d.js", i))
		err := os.WriteFile(scriptPath, []byte(simpleServerScript), 0755)
		if err != nil {
			t.Fatalf("Failed to create script copy %d: %v", i, err)
		}
		files[i] = scriptPath
	}

	// Test that each file gets a unique host:port
	hostPorts := make([]string, 0, len(files))
	for _, file := range files {
		hostPort, err := pm.getOrCreateHost(file)
		if err != nil {
			t.Fatalf("Failed to get host:port for %s: %v", file, err)
		}
		hostPorts = append(hostPorts, hostPort)
	}

	// Verify all host:ports are unique
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
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),          // idle timeout
		caddy.Duration(100*time.Millisecond), // startup timeout
		logger,
	)
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

func TestProcess_CrashDetection(t *testing.T) {
	logger := zap.NewNop()

	// Create a process that will crash (exit with code 1)
	process := &Process{
		Command:  "sh",
		Host:     "localhost",
		Port:     12346,
		LastUsed: time.Now(),
		onExit:   func() {},
		logger:   logger,
	}

	// Override the command args to make it crash
	process.mu.Lock()
	// We need to manually set this up since start() would construct normal host/port args
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

func TestProcessManager_ProcessExitCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zap.NewNop()
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),          // idle timeout
		caddy.Duration(500*time.Millisecond), // startup timeout - longer for server startup
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Create a Deno script that starts a server but exits after a short delay
	tmpDir := t.TempDir()
	exitScript := filepath.Join(tmpDir, "exit.js")
	scriptContent := `#!/usr/bin/env -S deno run --allow-net
const [host, port] = Deno.args;

// Start server
const server = Deno.serve({
  hostname: host,
  port: parseInt(port)
}, () => new Response("OK"));

console.log("Server started, will exit with code 42 after 100ms");

// Exit after short delay with code 42
setTimeout(() => {
  server.shutdown();
  Deno.exit(42);
}, 100);
`
	err = os.WriteFile(exitScript, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create exit script: %v", err)
	}

	// Get host:port for the script - this will start the process
	hostPort, err := pm.getOrCreateHost(exitScript)
	if err != nil {
		t.Fatalf("Failed to get host:port: %v", err)
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

	// Verify we got a valid host:port initially
	if hostPort == "" {
		t.Error("Host:port should not be empty")
	}
}

func TestProcessManager_NormalExitCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zap.NewNop()
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),          // idle timeout
		caddy.Duration(500*time.Millisecond), // startup timeout - longer for server startup
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Create a Deno script that starts a server but exits normally after a short delay
	tmpDir := t.TempDir()
	normalScript := filepath.Join(tmpDir, "normal.js")
	scriptContent := `#!/usr/bin/env -S deno run --allow-net
const [host, port] = Deno.args;

// Start server
const server = Deno.serve({
  hostname: host,
  port: parseInt(port)
}, () => new Response("OK"));

console.log("Server started, will exit normally after 100ms");

// Exit normally after short delay
setTimeout(() => {
  server.shutdown();
  Deno.exit(0);
}, 100);
`
	err = os.WriteFile(normalScript, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create normal exit script: %v", err)
	}

	// Get host:port for the script - this will start the process
	hostPort, err := pm.getOrCreateHost(normalScript)
	if err != nil {
		t.Fatalf("Failed to get host:port: %v", err)
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

	// Verify we got a valid host:port initially
	if hostPort == "" {
		t.Error("Host:port should not be empty")
	}
}

func TestValidateFilePath_Symlink(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a valid executable file
	realFile := filepath.Join(tmpDir, "test.sh")
	err := os.WriteFile(realFile, []byte("#!/bin/bash\necho hello"), 0755)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a symlink to the executable file
	symlinkPath := filepath.Join(tmpDir, "test_symlink.sh")
	err = os.Symlink(realFile, symlinkPath)
	if err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Test that symlink to valid executable passes validation
	err = validateFilePath(symlinkPath)
	if err != nil {
		t.Errorf("Symlink to executable should pass validation: %v", err)
	}

	// Create a broken symlink
	brokenSymlink := filepath.Join(tmpDir, "broken_symlink.sh")
	err = os.Symlink("/nonexistent/target", brokenSymlink)
	if err != nil {
		t.Fatalf("Failed to create broken symlink: %v", err)
	}

	// Test that broken symlink fails validation
	err = validateFilePath(brokenSymlink)
	if err == nil {
		t.Error("Broken symlink should fail validation")
	}

	// Create a symlink to a non-executable file
	nonExecFile := filepath.Join(tmpDir, "nonexec.txt")
	err = os.WriteFile(nonExecFile, []byte("content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create non-executable file: %v", err)
	}

	nonExecSymlink := filepath.Join(tmpDir, "nonexec_symlink.txt")
	err = os.Symlink(nonExecFile, nonExecSymlink)
	if err != nil {
		t.Fatalf("Failed to create symlink to non-executable: %v", err)
	}

	// Test that symlink to non-executable passes validateFilePath (executable check happens later)
	err = validateFilePath(nonExecSymlink)
	if err != nil {
		t.Errorf("Symlink to non-executable should pass validateFilePath: %v", err)
	}
}

func TestProcessManager_GetOrCreateHost_Symlink(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zap.NewNop()
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),
		caddy.Duration(3*time.Second), // Longer timeout for server startup
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a valid Deno HTTP server script
	realScript := filepath.Join(tmpDir, "test_server.js")
	scriptContent := `#!/usr/bin/env -S deno run --allow-net
const [host, port] = Deno.args;

const server = Deno.serve({
  hostname: host,
  port: parseInt(port)
}, () => new Response("Hello from symlinked script!"));

console.log("Symlinked server started");

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  server.shutdown();
  Deno.exit(0);
});
`
	err = os.WriteFile(realScript, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	// Create a symlink to the script
	symlinkPath := filepath.Join(tmpDir, "symlinked_server.js")
	err = os.Symlink(realScript, symlinkPath)
	if err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Test that process manager can handle symlinked scripts
	hostPort, err := pm.getOrCreateHost(symlinkPath)
	if err != nil {
		t.Fatalf("Failed to get host:port for symlinked script: %v", err)
	}

	if hostPort == "" {
		t.Error("Host:port should not be empty for symlinked script")
	}

	// Verify the process was created and stored under the symlink path (not resolved path)
	pm.mu.RLock()
	_, exists := pm.processes[symlinkPath]
	pm.mu.RUnlock()

	if !exists {
		t.Error("Process should exist in manager for symlinked script path")
	}
}
