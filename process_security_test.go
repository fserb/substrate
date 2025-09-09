//go:build !windows
// +build !windows

package substrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestConfigureProcessSecurity_NonRoot(t *testing.T) {
	// Test the common case: not running as root
	if os.Getuid() == 0 {
		t.Skip("Test should not be run as root")
	}

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test_script.sh")
	
	scriptContent := "#!/bin/bash\necho 'hello world'\n"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	cmd := exec.Command(scriptPath)
	err = configureProcessSecurity(cmd, scriptPath)
	
	// Should not error when not running as root
	if err != nil {
		t.Errorf("Unexpected error when not running as root: %v", err)
	}

	// Should not set credential when not running as root
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Errorf("Should not set credential when not running as root")
	}
}

func TestGetFileOwnership(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_file")
	
	err := os.WriteFile(testFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	uid, gid, err := getFileOwnership(testFile)
	if err != nil {
		t.Fatalf("Failed to get file ownership: %v", err)
	}

	// Verify by comparing with syscall.Stat
	fileInfo, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	stat := fileInfo.Sys().(*syscall.Stat_t)
	if uid != stat.Uid {
		t.Errorf("Expected UID %d, got %d", stat.Uid, uid)
	}
	if gid != stat.Gid {
		t.Errorf("Expected GID %d, got %d", stat.Gid, gid)
	}
}

func TestGetFileOwnership_NonexistentFile(t *testing.T) {
	_, _, err := getFileOwnership("/nonexistent/file")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}