package substrate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

func TestSubstrateTransport_GetOrStartProcess_Integration(t *testing.T) {
	// Skip integration test if running in short mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a simple test script that starts an HTTP server
	tempDir, err := os.MkdirTemp("", "substrate-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a simple Deno script that starts an HTTP server
	scriptContent := `#!/usr/bin/env -S deno run --allow-net
// Simple HTTP server for testing substrate transport

const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: test-server.js <host> <port>");
  Deno.exit(1);
}

const server = Deno.serve({
  hostname: host,
  port: parseInt(port)
}, (req) => {
  return new Response("Hello from substrate process!", {
    headers: { "Content-Type": "text/plain" }
  });
});

console.log(` + "`Server running at http://${host}:${port}/`" + `);

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Received SIGTERM, shutting down gracefully");
  server.shutdown();
  Deno.exit(0);
});
`

	scriptPath := filepath.Join(tempDir, "test-server.js")
	err = os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to write test script: %v", err)
	}

	// Setup transport
	transport := &SubstrateTransport{
		IdleTimeout:    caddy.Duration(60 * time.Second),
		StartupTimeout: caddy.Duration(5 * time.Second),
	}

	ctx := caddy.Context{
		Context: context.Background(),
	}

	err = transport.Provision(ctx)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	defer transport.Cleanup()

	// Create a request with replacer containing the script path
	req := httptest.NewRequest("GET", "/test-server.js", nil)
	repl := caddy.NewReplacer()
	repl.Set("http.matchers.file.absolute", scriptPath)
	reqCtx := context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl)
	req = req.WithContext(reqCtx)

	// Test getOrCreateHost directly
	filePath := scriptPath
	hostPort, err := transport.manager.getOrCreateHost(filePath)
	if err != nil {
		t.Fatalf("getOrCreateHost failed: %v", err)
	}

	if hostPort == "" {
		t.Fatal("Host:port should not be empty")
	}

	// Wait a moment for the server to start
	time.Sleep(200 * time.Millisecond)

	// Try to make an HTTP request to the started process (optional verification)
	testURL := fmt.Sprintf("http://%s/", hostPort)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(testURL)
	if err != nil {
		t.Logf("Could not connect to started process at %s: %v (this might be expected)", testURL, err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}
	}
}

func TestSymlinkExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	transport := setupTestTransport(t)
	defer transport.Cleanup()

	// Create a temporary directory for the symlink
	tempDir, err := os.MkdirTemp("", "substrate-symlink-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Get the original test script
	originalScript := filepath.Join(tempDir, "original_server.js")
	err = os.WriteFile(originalScript, []byte(simpleServerScript), 0755)
	if err != nil {
		t.Fatalf("Failed to create original script: %v", err)
	}

	// Create a symlink to the original script
	symlinkPath := filepath.Join(tempDir, "symlinked_server.js")
	err = os.Symlink(originalScript, symlinkPath)
	if err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Test getOrCreateHost with symlinked script
	hostPort, err := transport.manager.getOrCreateHost(symlinkPath)
	if err != nil {
		t.Fatalf("Failed to get host:port for symlinked script: %v", err)
	}

	if !strings.Contains(hostPort, "localhost:") {
		t.Errorf("Expected host:port to contain localhost:, got %s", hostPort)
	}

	time.Sleep(200 * time.Millisecond)

	// Make HTTP request to verify server is running and functioning correctly
	resp, err := http.Get(fmt.Sprintf("http://%s/test", hostPort))
	if err != nil {
		t.Fatalf("Failed to connect to symlinked server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Hello from substrate process") {
		t.Errorf("Response body should contain expected text from symlinked script, got: %s", bodyStr)
	}

	// Verify the process is tracked under the symlink path, not the resolved path
	transport.manager.mu.RLock()
	_, exists := transport.manager.processes[symlinkPath]
	transport.manager.mu.RUnlock()

	if !exists {
		t.Error("Process should be tracked under symlink path")
	}

	// Verify original script path is not used as key
	transport.manager.mu.RLock()
	_, originalExists := transport.manager.processes[originalScript]
	transport.manager.mu.RUnlock()

	if originalExists {
		t.Error("Process should not be tracked under original script path when accessed via symlink")
	}
}

// Helper functions

func setupTestTransport(t *testing.T) *SubstrateTransport {
	transport := &SubstrateTransport{
		IdleTimeout:    caddy.Duration(60 * time.Second),
		StartupTimeout: caddy.Duration(3 * time.Second),
	}

	ctx := caddy.Context{Context: context.Background()}
	err := transport.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision transport: %v", err)
	}

	return transport
}
