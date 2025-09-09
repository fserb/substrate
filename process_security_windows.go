//go:build windows
// +build windows

package substrate

import (
	"os/exec"
)

// configureProcessSecurity is a no-op on Windows
// Windows process security is handled differently through ACLs
func configureProcessSecurity(cmd *exec.Cmd, filePath string) error {
	// Windows doesn't use Unix-style UID/GID privilege dropping
	// Process security is managed through Access Control Lists (ACLs)
	return nil
}

