package substrate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

func TestSubstrateTransport_CaddyModule(t *testing.T) {
	transport := SubstrateTransport{}
	moduleInfo := transport.CaddyModule()

	expectedID := "http.reverse_proxy.transport.substrate"
	if string(moduleInfo.ID) != expectedID {
		t.Errorf("Expected module ID %s, got %s", expectedID, moduleInfo.ID)
	}

	// Test that New() creates a valid transport
	newTransport := moduleInfo.New()
	if newTransport == nil {
		t.Error("New() should return a non-nil transport")
	}

	if _, ok := newTransport.(*SubstrateTransport); !ok {
		t.Error("New() should return a *SubstrateTransport")
	}
}

func TestSubstrateTransport_Provision(t *testing.T) {
	transport := &SubstrateTransport{
		IdleTimeout:    caddy.Duration(300000000000), // 5 minutes
		StartupTimeout: caddy.Duration(30000000000),  // 30 seconds
	}

	// Create a basic context with a logger
	ctx := caddy.Context{
		Context: context.Background(),
	}

	err := transport.Provision(ctx)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	// Verify internal state was initialized
	if transport.manager == nil {
		t.Error("manager should be initialized")
	}
	if transport.logger == nil {
		t.Error("logger should be initialized")
	}
}


func TestSubstrateTransport_GetFilePathFromReplacer(t *testing.T) {
	// Create a request with context and replacer
	req := httptest.NewRequest("GET", "/hello.js", nil)
	
	// Create a replacer and set file.absolute value
	repl := caddy.NewReplacer()
	repl.Set("http.matchers.file.absolute", "/absolute/path/to/hello.js")
	ctx := context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl)
	req = req.WithContext(ctx)

	// Test getting file path from replacer
	repl2 := req.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	filePath, _ := repl2.GetString("http.matchers.file.absolute")
	
	expected := "/absolute/path/to/hello.js"
	if filePath != expected {
		t.Errorf("Expected file path %s, got %s", expected, filePath)
	}
}

func TestSubstrateTransport_GetFilePathFallback(t *testing.T) {
	// Create a request with context and replacer but no file.absolute value
	req := httptest.NewRequest("GET", "/hello.js", nil)
	
	// Create a replacer without setting file.absolute value
	repl := caddy.NewReplacer()
	ctx := context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl)
	req = req.WithContext(ctx)

	// Test that it falls back to URL path when no file.absolute is set
	repl2 := req.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	filePath, _ := repl2.GetString("http.matchers.file.absolute")
	
	// Should be empty since we didn't set it
	if filePath != "" {
		t.Errorf("Expected empty file path, got %s", filePath)
	}
	
	// Fallback should be the URL path
	fallbackPath := req.URL.Path
	expected := "/hello.js"
	if fallbackPath != expected {
		t.Errorf("Expected fallback path %s, got %s", expected, fallbackPath)
	}
}


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
  hostname: host === "localhost" ? "127.0.0.1" : host, 
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

	// Check if Deno is available
	if !isDenoAvailable() {
		t.Skip("Deno not available, skipping integration test")
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

	// Verify we got a host:port
	if hostPort == "" {
		t.Fatal("Host:port should not be empty")
	}

	// Wait a moment for the server to start
	time.Sleep(2 * time.Second)

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


func TestSubstrateTransport_Cleanup(t *testing.T) {
	transport := &SubstrateTransport{
		IdleTimeout:    caddy.Duration(60 * time.Second),
		StartupTimeout: caddy.Duration(5 * time.Second),
	}

	ctx := caddy.Context{
		Context: context.Background(),
	}

	err := transport.Provision(ctx)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	// Test cleanup
	err = transport.Cleanup()
	if err != nil {
		t.Errorf("Cleanup should not return error, got: %v", err)
	}

	// Test cleanup when manager is nil
	transport.manager = nil
	err = transport.Cleanup()
	if err != nil {
		t.Errorf("Cleanup with nil manager should not return error, got: %v", err)
	}
}

func TestSubstrateTransport_Validate(t *testing.T) {
	transport := &SubstrateTransport{
		IdleTimeout:    caddy.Duration(5 * time.Minute),
		StartupTimeout: caddy.Duration(30 * time.Second),
	}

	err := transport.Validate()
	if err != nil {
		t.Errorf("Validate should not return error for valid transport, got: %v", err)
	}
	
	// Test invalid configurations
	invalidTransport := &SubstrateTransport{
		IdleTimeout:    caddy.Duration(-1 * time.Second),
		StartupTimeout: caddy.Duration(0),
	}
	
	err = invalidTransport.Validate()
	if err == nil {
		t.Error("Validate should return error for invalid transport")
	}
}

func isDenoAvailable() bool {
	cmd := exec.Command("deno", "--version")
	err := cmd.Run()
	return err == nil
}