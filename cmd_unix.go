//go:build !windows

package substrate

import (
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// configureSysProcAttr sets up process group attributes to ensure
// proper process management and signal handling on Unix systems.
func configureSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
}

// configureExecutingUser changes the user context for command execution
// when the specified username differs from the current user.
// This allows commands to run with the permissions of a different user.
func configureExecutingUser(cmd *exec.Cmd, username string) {
	if username != "" {
		currentUser, _ := user.Current()

		if currentUser.Username != username {
			executingUser, _ := user.Lookup(username)

			uid, _ := strconv.Atoi(executingUser.Uid)
			gid, _ := strconv.Atoi(executingUser.Gid)

			cmd.SysProcAttr.Credential = &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			}
		}
	}
}
