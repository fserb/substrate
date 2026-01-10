package substrate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestConfigureProcessSecurity_NonRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Test should not be run as root")
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_script.js")

	// Plain text file - no shebang or executable permission needed
	err := os.WriteFile(filePath, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cmd := exec.Command("deno", "run", filePath)
	err = configureProcessSecurity(cmd, filePath)

	if err != nil {
		t.Errorf("Unexpected error when not running as root: %v", err)
	}

	// Should not set credential when not running as root
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Errorf("Should not set credential when not running as root")
	}
}

func TestConfigureProcessSecurity_FilePermissions(t *testing.T) {
	// Since we run scripts via deno, executable permission is not required
	// configureProcessSecurity should pass regardless of file permissions
	tmpDir := t.TempDir()

	testCases := []struct {
		name string
		mode os.FileMode
	}{
		{"readable_0644", 0644},
		{"readonly_0444", 0444},
		{"executable_0755", 0755},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, tc.name+".js")

			// Plain text file - content doesn't matter for this test
			err := os.WriteFile(filePath, []byte("test content"), tc.mode)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			cmd := exec.Command("deno", "run", filePath)
			err = configureProcessSecurity(cmd, filePath)

			if err != nil {
				t.Errorf("Unexpected error for file with mode %o: %v", tc.mode, err)
			}
		})
	}
}

func TestConfigureProcessSecurity_NonExistentFile(t *testing.T) {
	// When not running as root, configureProcessSecurity returns early without checking file
	// So non-existent files only fail when running as root (stat fails)
	if os.Getuid() != 0 {
		t.Skip("Test only relevant when running as root")
	}

	nonExistentPath := "/path/that/does/not/exist.js"

	cmd := exec.Command("deno", "run", nonExistentPath)
	err := configureProcessSecurity(cmd, nonExistentPath)

	if err == nil {
		t.Errorf("Expected error for non-existent file when running as root, but got none")
	}
}

func TestConfigureProcessSecurity_Symlink(t *testing.T) {
	// Test that symlinks to files are handled correctly
	// Symlinks are transparent since Deno just reads whatever the path resolves to
	tmpDir := t.TempDir()

	// Create a plain text file
	realFile := filepath.Join(tmpDir, "real_script.js")
	err := os.WriteFile(realFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Create a symlink to the file
	symlinkPath := filepath.Join(tmpDir, "symlink_script.js")
	err = os.Symlink(realFile, symlinkPath)
	if err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	cmd := exec.Command("deno", "run", symlinkPath)
	err = configureProcessSecurity(cmd, symlinkPath)

	if err != nil {
		t.Errorf("Unexpected error for symlinked file: %v", err)
	}
}
