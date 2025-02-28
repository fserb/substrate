//go:build !windows

package substrate

import (
	"os/exec"
	"os/user"
	"strings"
	"testing"
)

// TestConfigureSysProcAttr tests the configureSysProcAttr function
func TestCmdConfigureSysProcAttr(t *testing.T) {
	cmd := exec.Command("echo", "test")

	// Configure the process attributes
	configureSysProcAttr(cmd)

	// Verify the attributes were set correctly
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}

	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid is not set")
	}

	if cmd.SysProcAttr.Pgid != 0 {
		t.Errorf("Pgid = %d, want 0", cmd.SysProcAttr.Pgid)
	}
}

// TestConfigureExecutingUser tests the configureExecutingUser function
func TestCmdConfigureExecutingUser(t *testing.T) {
	// Test with empty username
	t.Run("EmptyUsername", func(t *testing.T) {
		cmd := exec.Command("echo", "test")
		configureSysProcAttr(cmd)

		configureExecutingUser(cmd, "")

		if cmd.SysProcAttr.Credential != nil {
			t.Error("Credential should be nil for empty username")
		}
	})

	// Test with current user
	t.Run("CurrentUser", func(t *testing.T) {
		currentUser, err := user.Current()
		if err != nil {
			t.Fatalf("Failed to get current user: %v", err)
		}

		cmd := exec.Command("whoami")
		configureSysProcAttr(cmd)

		configureExecutingUser(cmd, currentUser.Username)

		if cmd.SysProcAttr.Credential != nil {
			t.Error("Credential should be nil for current user")
		}

		// Run the command to verify it executes as the current user
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("Command failed: %v", err)
		}

		got := strings.TrimSpace(string(out))
		if got != currentUser.Username {
			t.Errorf("Command ran as %q, want %q", got, currentUser.Username)
		}
	})
}

// TestCommandOutputSwitchUser tests switching to a different user
// This test requires root privileges
func TestCmdCommandOutputSwitchUser(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	// Skip if not running as root
	if currentUser.Username != "root" {
		t.Skip("skipping switch user test; must be run as root")
	}

	// Test switching to nobody user
	targetUser := "nobody" // assuming "nobody" exists

	// Look up the target user
	_, err = user.Lookup(targetUser)
	if err != nil {
		t.Skipf("skipping; user %q not found: %v", targetUser, err)
	}

	cmd := exec.Command("whoami")
	configureSysProcAttr(cmd)
	configureExecutingUser(cmd, targetUser)

	// Verify credential was set
	if cmd.SysProcAttr.Credential == nil {
		t.Fatal("Credential is nil")
	}

	// Run the command
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	got := strings.TrimSpace(string(out))
	if got != targetUser {
		t.Errorf("Command ran as %q, want %q", got, targetUser)
	}
}
