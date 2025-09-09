package substrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

func TestProcessManager_GetOrCreateHost(t *testing.T) {
	logger := zap.NewNop()
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),
		caddy.Duration(100*time.Millisecond),
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	filePath := "/bin/echo"
	hostPort, err := pm.getOrCreateHost(filePath)
	if err != nil {
		t.Fatalf("Failed to get host:port: %v", err)
	}

	if hostPort == "" {
		t.Error("Host:port should not be empty")
	}
	if len(hostPort) < 10 {
		t.Errorf("Host:port format looks incorrect: %s", hostPort)
	}
}

func TestProcessManager_MultipleProcesses(t *testing.T) {
	logger := zap.NewNop()
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),
		caddy.Duration(100*time.Millisecond),
		logger,
	)
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
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),
		caddy.Duration(100*time.Millisecond),
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Test that calling getOrCreateHost twice for same file
	// Since sleep exits immediately with no args, we'll test the creation behavior
	file := "/bin/sleep"

	hostPort1, err := pm.getOrCreateHost(file)
	if err != nil {
		t.Fatalf("Failed to get host:port first time: %v", err)
	}

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

func TestProcess_Stop(t *testing.T) {
	logger := zap.NewNop()

	process := &Process{
		Command:  "/bin/sleep",
		Host:     "localhost",
		Port:     12345,
		LastUsed: time.Now(),
		onExit:   func() {},
		logger:   logger,
	}

	err := process.start()
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	err = process.Stop()
	if err != nil {
		// For sleep processes, termination signals are expected
		t.Logf("Process stop returned error (expected for SIGTERM): %v", err)
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

	// Create a temporary script that exits with code 42
	tmpDir := t.TempDir()
	exitScript := filepath.Join(tmpDir, "exit.sh")
	err = os.WriteFile(exitScript, []byte("#!/bin/bash\nexit 42"), 0755)
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
	maxWait := 2 * time.Second
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

	// Create a temporary script that exits normally (code 0)
	tmpDir := t.TempDir()
	normalScript := filepath.Join(tmpDir, "normal.sh")
	err = os.WriteFile(normalScript, []byte("#!/bin/bash\nexit 0"), 0755)
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
	time.Sleep(200 * time.Millisecond)

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

func TestProcess_NormalExitLogging(t *testing.T) {
	logger := zap.NewNop()

	// Create a process that exits normally (exit code 0)
	process := &Process{
		Command:  "sh",
		Host:     "localhost",
		Port:     12347,
		LastUsed: time.Now(),
		onExit:   func() {},
		logger:   logger,
	}

	// Override the command args to make it exit normally
	process.mu.Lock()
	process.Cmd = exec.Command("sh", "-c", "exit 0")
	process.mu.Unlock()

	// Start the command directly
	if err := process.Cmd.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Start monitoring
	go process.monitor()

	// Wait a moment for the process to exit
	time.Sleep(100 * time.Millisecond)

	// Check the exit code was captured as success
	process.mu.RLock()
	exitCode := process.exitCode
	process.mu.RUnlock()

	if exitCode != 0 {
		t.Errorf("Expected exit code 0 (normal), got %d", exitCode)
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
	logger := zap.NewNop()
	pm, err := NewProcessManager(
		caddy.Duration(time.Minute),
		caddy.Duration(100*time.Millisecond),
		logger,
	)
	if err != nil {
		t.Fatalf("Failed to create process manager: %v", err)
	}
	defer pm.Stop()

	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a valid executable script
	realScript := filepath.Join(tmpDir, "test_script.sh")
	scriptContent := "#!/bin/bash\necho 'test output'"
	err = os.WriteFile(realScript, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	// Create a symlink to the script
	symlinkPath := filepath.Join(tmpDir, "symlinked_script.sh")
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
