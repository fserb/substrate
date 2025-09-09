//go:build !windows
// +build !windows

package substrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	
	// Should not error when not running as root with executable file
	if err != nil {
		t.Errorf("Unexpected error when not running as root: %v", err)
	}

	// Should not set credential when not running as root
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Errorf("Should not set credential when not running as root")
	}
}

func TestConfigureProcessSecurity_NonExecutableFile(t *testing.T) {
	// Test that non-executable files are rejected regardless of user
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "non_executable.txt")
	
	// Create a non-executable file (mode 0644)
	scriptContent := "#!/bin/bash\necho 'hello world'\n"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cmd := exec.Command(scriptPath)
	err = configureProcessSecurity(cmd, scriptPath)
	
	// Should error because file is not executable
	if err == nil {
		t.Errorf("Expected error for non-executable file, but got none")
	}
	
	if err != nil && !strings.Contains(err.Error(), "not executable") {
		t.Errorf("Expected error to mention 'not executable', got: %v", err)
	}
}

func TestConfigureProcessSecurity_ExecutableFile(t *testing.T) {
	// Test that executable files pass the check
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "executable.sh")
	
	// Create an executable file (mode 0755)
	scriptContent := "#!/bin/bash\necho 'hello world'\n"
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	cmd := exec.Command(scriptPath)
	err = configureProcessSecurity(cmd, scriptPath)
	
	// Should not error for executable file
	if err != nil {
		t.Errorf("Unexpected error for executable file: %v", err)
	}
}

func TestConfigureProcessSecurity_NonExistentFile(t *testing.T) {
	// Test that non-existent files are handled properly
	nonExistentPath := "/path/that/does/not/exist"
	
	cmd := exec.Command(nonExistentPath)
	err := configureProcessSecurity(cmd, nonExistentPath)
	
	// Should error because file doesn't exist
	if err == nil {
		t.Errorf("Expected error for non-existent file, but got none")
	}
}

func TestConfigureProcessSecurity_Symlink(t *testing.T) {
	// Test that symlinks to executable files are handled correctly
	tmpDir := t.TempDir()
	
	// Create an executable file
	realScript := filepath.Join(tmpDir, "real_script.sh")
	scriptContent := "#!/bin/bash\necho 'hello world'\n"
	err := os.WriteFile(realScript, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create real script: %v", err)
	}
	
	// Create a symlink to the executable file
	symlinkPath := filepath.Join(tmpDir, "symlink_script.sh")
	err = os.Symlink(realScript, symlinkPath)
	if err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}
	
	cmd := exec.Command(symlinkPath)
	err = configureProcessSecurity(cmd, symlinkPath)
	
	// Should not error for symlinked executable file
	if err != nil {
		t.Errorf("Unexpected error for symlinked executable file: %v", err)
	}
	
	// Test symlink to non-executable file
	nonExecFile := filepath.Join(tmpDir, "non_exec.txt")
	err = os.WriteFile(nonExecFile, []byte("content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create non-executable file: %v", err)
	}
	
	nonExecSymlink := filepath.Join(tmpDir, "non_exec_symlink.txt")
	err = os.Symlink(nonExecFile, nonExecSymlink)
	if err != nil {
		t.Fatalf("Failed to create symlink to non-executable: %v", err)
	}
	
	cmd2 := exec.Command(nonExecSymlink)
	err = configureProcessSecurity(cmd2, nonExecSymlink)
	
	// Should error because symlink target is not executable
	if err == nil {
		t.Errorf("Expected error for symlink to non-executable file, but got none")
	}
	
	if err != nil && !strings.Contains(err.Error(), "not executable") {
		t.Errorf("Expected error to mention 'not executable', got: %v", err)
	}
}


