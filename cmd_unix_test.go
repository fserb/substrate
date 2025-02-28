//go:build !windows

package substrate

import (
	"os/exec"
	"os/user"
	"strings"
	"testing"
)

func TestCmdUnix_CommandOutput_CurrentUser(t *testing.T) {

	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("whoami")
	// When the username matches the current user, credentials shouldn't be altered.
	configureSysProcAttr(cmd)
	configureExecutingUser(cmd, currentUser.Username)

	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(string(out))
	if got != currentUser.Username {
		t.Errorf("expected user %q, got %q", currentUser.Username, got)
	}
}

func TestCmdUnix_CommandOutput_SwitchUser(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	// Switching users requires root privileges.
	if currentUser.Username != "root" {
		t.Skip("skipping switch user test; must be run as root")
	}

	targetUser := "nobody" // assuming "nobody" exists
	cmd := exec.Command("whoami")
	configureSysProcAttr(cmd)
	configureExecutingUser(cmd, targetUser)

	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(string(out))
	if got != targetUser {
		t.Errorf("expected user %q, got %q", targetUser, got)
	}
}
