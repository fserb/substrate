package e2e

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/caddytest"

	_ "github.com/fserb/substrate"
)

type TestFile struct {
	Path    string
	Content string
	Mode    os.FileMode // Optional, 0 defaults to 0644 (ug+rw,o+r)
}

// Template variables available in Caddyfile:
// {{TEMPDIR}}   - Absolute path to temporary test directory
// {{GLOBAL}}    - Global configuration block with admin and http_port
// {{HTTP_PORT}} - HTTP port for site blocks

type E2ETestContext struct {
	T                *testing.T
	TempDir          string
	Tester           *caddytest.Tester
	BaseURL          string
	HTTPPort         int
	ExpectedResponse string
}

func (ctx *E2ETestContext) TearDown() {
	if ctx.TempDir != "" {
		os.RemoveAll(ctx.TempDir)
	}
}

func RunE2ETest(t *testing.T, caddyfile string, files []TestFile) *E2ETestContext {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "substrate-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Write all test files to temp directory
	for _, file := range files {
		filePath := filepath.Join(tempDir, file.Path)

		// Create directory if needed
		if dir := filepath.Dir(filePath); dir != tempDir {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("Failed to create directory %s: %v", dir, err)
			}
		}

		// Use default mode if not specified (0)
		mode := file.Mode
		if mode == 0 {
			mode = 0644 // ug+rw,o+r
		}

		if err := os.WriteFile(filePath, []byte(file.Content), mode); err != nil {
			t.Fatalf("Failed to write file %s: %v", filePath, err)
		}
	}

	// Get free port for HTTP (admin uses standard CaddyTest port 2999)
	httpPort, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free HTTP port: %v", err)
	}

	// Replace template variables in Caddyfile
	processedCaddyfile := strings.ReplaceAll(caddyfile, "{{TEMPDIR}}", tempDir)
	processedCaddyfile = strings.ReplaceAll(processedCaddyfile, "{{GLOBAL}}", fmt.Sprintf("admin localhost:2999\n\thttp_port %d", httpPort))
	processedCaddyfile = strings.ReplaceAll(processedCaddyfile, "{{HTTP_PORT}}", fmt.Sprintf("%d", httpPort))

	// Create and initialize Caddy tester
	tester := caddytest.NewTester(t)
	tester.InitServer(processedCaddyfile, "caddyfile")

	// Create and return context
	ctx := &E2ETestContext{
		T:        t,
		TempDir:  tempDir,
		Tester:   tester,
		BaseURL:  fmt.Sprintf("http://localhost:%d", httpPort),
		HTTPPort: httpPort,
	}

	return ctx
}

func getFreePort() (int, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("failed to find free port: %w", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to get TCP address")
	}

	return addr.Port, nil
}
