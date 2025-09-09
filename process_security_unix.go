//go:build !windows
// +build !windows

package substrate

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"syscall"

	"golang.org/x/sys/unix"
)

// configureProcessSecurity sets up process security by dropping privileges
// to match the file owner's user and group when running as root
func configureProcessSecurity(cmd *exec.Cmd, filePath string) error {
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("failed to get current user: %w", err)
	}

	if currentUser.Uid != "0" {
		return nil
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	stat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("failed to get file system info for %s", filePath)
	}

	fileUID := stat.Uid
	fileGID := stat.Gid

	if fileUID == 0 {
		return nil
	}

	if err := unix.Access(filePath, unix.X_OK); err != nil {
		return fmt.Errorf("file %s is not executable: %w", filePath, err)
	}

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	cmd.SysProcAttr.Credential = &syscall.Credential{
		Uid: fileUID,
		Gid: fileGID,
	}

	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0

	return nil
}


