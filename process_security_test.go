//go:build !windows
// +build !windows

package substrate

import (
	"os"
	"os/exec"
	"path/filepath"
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

