package e2e

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddytest"
	_ "github.com/fserb/substrate"
)

// SubstrateConfig holds optional configuration for the substrate transport.
type SubstrateConfig struct {
	IdleTimeout    string // e.g., "5m", "0", "-1"
	StartupTimeout string // e.g., "30s"
}

// StandardServerBlock returns the default server block for substrate tests.
// It matches *.js files and routes them through the substrate transport.
func StandardServerBlock() string {
	return ServerBlockWithConfig(SubstrateConfig{})
}

// ServerBlockWithConfig returns a server block with the specified substrate configuration.
func ServerBlockWithConfig(cfg SubstrateConfig) string {
	var transportConfig string
	if cfg.IdleTimeout != "" || cfg.StartupTimeout != "" {
		var opts []string
		if cfg.IdleTimeout != "" {
			opts = append(opts, fmt.Sprintf("idle_timeout %s", cfg.IdleTimeout))
		}
		if cfg.StartupTimeout != "" {
			opts = append(opts, fmt.Sprintf("startup_timeout %s", cfg.StartupTimeout))
		}
		transportConfig = fmt.Sprintf(" {\n\t\t\t%s\n\t\t}", strings.Join(opts, "\n\t\t\t"))
	}

	return fmt.Sprintf(`@js_files {
	path *.js
	file {path}
}

reverse_proxy @js_files {
	transport substrate%s
	to localhost
}`, transportConfig)
}

type TestFile struct {
	Path    string
	Content string
	Mode    os.FileMode // Optional, 0 defaults to 0644
}

type E2ETestContext struct {
	T                *testing.T
	TempDir          string
	Tester           *caddytest.Tester
	BaseURL          string
	HTTPPort         int
	ExpectedResponse string
}

func (ctx *E2ETestContext) AssertGet(path, expectedBody string) {
	ctx.Tester.AssertGetResponse(ctx.BaseURL+path, 200, expectedBody)
}

func (ctx *E2ETestContext) AssertGetStatus(path string, expectedStatus int) {
	req, err := http.NewRequest("GET", ctx.BaseURL+path, nil)
	if err != nil {
		ctx.T.Fatalf("Failed to create request for %s: %v", path, err)
	}
	ctx.Tester.AssertResponseCode(req, expectedStatus)
}

func RunE2ETest(t *testing.T, serverBlockContent string, files []TestFile) *E2ETestContext {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "substrate-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	for _, file := range files {
		filePath := filepath.Join(tempDir, file.Path)

		if dir := filepath.Dir(filePath); dir != tempDir {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("Failed to create directory %s: %v", dir, err)
			}
		}

		mode := file.Mode
		if mode == 0 {
			mode = 0644
		}

		if err := os.WriteFile(filePath, []byte(file.Content), mode); err != nil {
			t.Fatalf("Failed to write file %s: %v", filePath, err)
		}
	}

	httpPort, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free HTTP port: %v", err)
	}

	adminPort, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free admin port: %v", err)
	}

	fullCaddyfile := fmt.Sprintf(`{
	admin localhost:%d
	http_port %d
	log {
		format console
		level ERROR
	}
}

:%d {
	root %s
	%s
}`, adminPort, httpPort, httpPort, tempDir, serverBlockContent)

	tester := caddytest.NewTester(t).WithDefaultOverrides(caddytest.Config{
		AdminPort: adminPort,
	})
	tester.InitServer(fullCaddyfile, "caddyfile")

	ctx := &E2ETestContext{
		T:        t,
		TempDir:  tempDir,
		Tester:   tester,
		BaseURL:  fmt.Sprintf("http://localhost:%d", httpPort),
		HTTPPort: httpPort,
	}

	t.Cleanup(func() {
		caddy.Stop()
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

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
