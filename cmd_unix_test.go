//go:build !windows

package substrate

import (
	"os/exec"
	"os/user"
	"strings"
	"testing"
)

func TestCmdConfigureSysProcAttr(t *testing.T) {
	cmd := exec.Command("echo", "test")

	configureSysProcAttr(cmd)

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

func TestCmdConfigureExecutingUser(t *testing.T) {
	t.Run("EmptyUsername", func(t *testing.T) {
		cmd := exec.Command("echo", "test")
		configureSysProcAttr(cmd)

		configureExecutingUser(cmd, "")

		if cmd.SysProcAttr.Credential != nil {
			t.Error("Credential should be nil for empty username")
		}
	})

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

// This test requires root privileges
func TestCmdCommandOutputSwitchUser(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	if currentUser.Username != "root" {
		t.Skip("skipping switch user test; must be run as root")
	}

	targetUser := "nobody"
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

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	got := strings.TrimSpace(string(out))
	if got != targetUser {
		t.Errorf("Command ran as %q, want %q", got, targetUser)
	}
}

