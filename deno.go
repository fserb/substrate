/*
Deno runtime management.

DenoManager downloads and caches the Deno binary for the current platform.
Substrate uses a specific Deno version to ensure consistent behavior.
The binary is cached in ~/.cache/substrate/deno/{version}-{platform}/.

This avoids requiring Deno to be pre-installed on the system.
*/
package substrate

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"go.uber.org/zap"
)

const (
	DenoVersion   = "v2.1.9"
	cacheBasePath = ".cache/substrate/deno"
)

// DenoManager handles downloading and caching of the Deno runtime
type DenoManager struct {
	version string
	rootDir string
	logger  *zap.Logger
}

// NewDenoManager creates a new DenoManager with the default version
func NewDenoManager(logger *zap.Logger) *DenoManager {
	rootDir := "/opt/homebrew/var/substrate/deno"
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		rootDir = filepath.Join(homeDir, cacheBasePath)
	}
	return &DenoManager{
		version: DenoVersion,
		rootDir: rootDir,
		logger:  logger,
	}
}

// Get returns the path to the Deno binary, downloading it if necessary
func (dm *DenoManager) Get() (string, error) {
	exePath := dm.executablePath()

	if dm.validateBinary(exePath) {
		return exePath, nil
	}

	if err := dm.download(); err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	if !dm.validateBinary(exePath) {
		return "", fmt.Errorf("downloaded binary validation failed")
	}

	return exePath, nil
}

func (dm *DenoManager) executablePath() string {
	platform := dm.platformString()
	return filepath.Join(dm.rootDir, dm.version+"-"+platform, "deno")
}

func (dm *DenoManager) platformString() string {
	switch runtime.GOOS {
	case "linux":
		return "x86_64-unknown-linux-gnu"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "aarch64-apple-darwin"
		}
		return "x86_64-apple-darwin"
	default:
		return "x86_64-unknown-linux-gnu"
	}
}

func (dm *DenoManager) downloadURL() string {
	platform := dm.platformString()
	return fmt.Sprintf(
		"https://github.com/denoland/deno/releases/download/%s/deno-%s.zip",
		dm.version, platform,
	)
}

func (dm *DenoManager) download() error {
	url := dm.downloadURL()

	dm.logger.Info("downloading deno", zap.String("url", url))

	cacheDir := filepath.Dir(dm.executablePath())
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	tmpFile := filepath.Join(cacheDir, "deno.zip.tmp")
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("write temp file: %w", err)
	}
	f.Close()

	if err := dm.extractZip(tmpFile, cacheDir); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("extract zip: %w", err)
	}

	os.Remove(tmpFile)

	exePath := dm.executablePath()
	if err := os.Chmod(exePath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	dm.logger.Info("downloaded deno", zap.String("version", dm.version))
	return nil
}

func (dm *DenoManager) extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, "deno") {
			continue
		}

		destPath := filepath.Join(destDir, f.Name)

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

func (dm *DenoManager) validateBinary(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	if !info.Mode().IsRegular() {
		return false
	}

	cmd := exec.Command(path, "--version")
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}
