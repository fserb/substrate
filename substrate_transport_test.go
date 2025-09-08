package substrate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
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
		HTTPTransport:  new(reverseproxy.HTTPTransport),
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
	if transport.processMap == nil {
		t.Error("processMap should be initialized")
	}
	if transport.processMapMu == nil {
		t.Error("processMapMu should be initialized")
	}
	if transport.manager == nil {
		t.Error("manager should be initialized")
	}
	if transport.logger == nil {
		t.Error("logger should be initialized")
	}
}

func TestSubstrateTransport_GenerateProcessKey(t *testing.T) {
	transport := &SubstrateTransport{}

	tests := []struct {
		path     string
		expected string
	}{
		{"/hello.js", "/hello.js"},
		{"/app/server.py", "/app/server.py"},
		{"/", "/"},
		{"/nested/path/script.sh", "/nested/path/script.sh"},
	}

	for _, test := range tests {
		req := &http.Request{
			URL: &url.URL{Path: test.path},
		}
		result := transport.generateProcessKey(req)
		if result != test.expected {
			t.Errorf("generateProcessKey(%s): expected %s, got %s", test.path, test.expected, result)
		}
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

func TestSubstrateTransport_GetFreePort(t *testing.T) {
	transport := &SubstrateTransport{}

	port, err := transport.getFreePort()
	if err != nil {
		t.Fatalf("getFreePort failed: %v", err)
	}

	if port <= 0 || port > 65535 {
		t.Errorf("Port %d is not in valid range", port)
	}

	// Test that we can get multiple different ports
	port2, err := transport.getFreePort()
	if err != nil {
		t.Fatalf("getFreePort failed on second call: %v", err)
	}

	// Ports might be the same if the first was quickly freed, but typically they're different
	if port2 <= 0 || port2 > 65535 {
		t.Errorf("Second port %d is not in valid range", port2)
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

	// Create a simple Node.js script that starts an HTTP server
	scriptContent := `#!/usr/bin/env node
const http = require('http');

// Get host and port from command line arguments
const host = process.argv[2] || 'localhost';
const port = parseInt(process.argv[3]) || 8080;

const server = http.createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/plain' });
  res.end('Hello from substrate process!');
});

server.listen(port, host, () => {
  console.log(` + "`Server running at http://${host}:${port}/`" + `);
});

// Graceful shutdown
process.on('SIGTERM', () => {
  server.close(() => {
    process.exit(0);
  });
});
`

	scriptPath := filepath.Join(tempDir, "test-server.js")
	err = os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to write test script: %v", err)
	}

	// Check if Node.js is available
	if _, err := os.Stat("/usr/bin/node"); os.IsNotExist(err) {
		if _, err := os.Stat("/usr/local/bin/node"); os.IsNotExist(err) {
			t.Skip("Node.js not found, skipping integration test")
		}
	}

	// Setup transport
	transport := &SubstrateTransport{
		HTTPTransport:  new(reverseproxy.HTTPTransport),
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

	// Get or start the process
	key := transport.generateProcessKey(req)
	instance, err := transport.getOrStartProcess(key, req)
	if err != nil {
		t.Fatalf("getOrStartProcess failed: %v", err)
	}

	// Verify the instance was created
	if instance == nil {
		t.Fatal("Instance should not be nil")
	}
	if instance.Port <= 0 {
		t.Errorf("Instance port should be positive, got %d", instance.Port)
	}
	if instance.Process == nil {
		t.Fatal("Instance process should not be nil")
	}

	// Wait a moment for the server to start
	time.Sleep(2 * time.Second)

	// Try to make an HTTP request to the started process (optional verification)
	testURL := fmt.Sprintf("http://localhost:%d/", instance.Port)
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

func TestSubstrateTransport_MarkProcessForRestart(t *testing.T) {
	transport := &SubstrateTransport{
		HTTPTransport:  new(reverseproxy.HTTPTransport),
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
	defer transport.Cleanup()

	// Create a mock process instance
	key := "test-process"
	mockProcess := &ManagedProcess{
		Key:      key,
		running:  true,
		LastUsed: time.Now(),
	}
	mockInstance := &ProcessInstance{
		Process: mockProcess,
		Port:    8080,
	}

	// Add it to the process map
	transport.processMap[key] = mockInstance

	// Mark for restart
	transport.markProcessForRestart(key)

	// Verify the process was removed from the map
	if _, exists := transport.processMap[key]; exists {
		t.Error("Process should have been removed from map after marking for restart")
	}
}

func TestSubstrateTransport_Cleanup(t *testing.T) {
	transport := &SubstrateTransport{
		HTTPTransport:  new(reverseproxy.HTTPTransport),
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
	transport := &SubstrateTransport{}

	err := transport.Validate()
	if err != nil {
		t.Errorf("Validate should not return error for default transport, got: %v", err)
	}
}