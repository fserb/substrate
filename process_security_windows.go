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

// getFileOwnership returns 0,0 on Windows as UID/GID don't apply
func getFileOwnership(filePath string) (uint32, uint32, error) {
	// Windows uses different security model - SIDs instead of UID/GID
	return 0, 0, nil
}