//go:build e2e

package substrate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

func TestE2E_SimpleServerStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Check if Deno is available
	if !isDenoAvailable() {
		t.Skip("Deno not available, skipping E2E test")
	}

	transport := setupTestTransport(t)
	defer transport.Cleanup()

	scriptPath := getTestScript(t, "simple_server.js")

	// Test getOrCreateHost
	hostPort, err := transport.manager.getOrCreateHost(scriptPath)
	if err != nil {
		t.Fatalf("Failed to get host:port: %v", err)
	}

	if !strings.Contains(hostPort, "localhost:") {
		t.Errorf("Expected host:port to contain localhost:, got %s", hostPort)
	}

	// Wait for server to start
	time.Sleep(2 * time.Second)

	// Make HTTP request to verify server is running
	resp, err := http.Get(fmt.Sprintf("http://%s/test", hostPort))
	if err != nil {
		t.Fatalf("Failed to connect to started server: %v", err)
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
		t.Errorf("Response body should contain expected text, got: %s", bodyStr)
	}
}

func TestE2E_MultipleProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	if !isDenoAvailable() {
		t.Skip("Deno not available, skipping E2E test")
	}

	transport := setupTestTransport(t)
	defer transport.Cleanup()

	// Start multiple different processes
	scripts := []string{"simple_server.js", "echo_server.js", "port_checker.js"}
	hostPorts := make([]string, len(scripts))

	for i, script := range scripts {
		scriptPath := getTestScript(t, script)
		hostPort, err := transport.manager.getOrCreateHost(scriptPath)
		if err != nil {
			t.Fatalf("Failed to get host:port for %s: %v", script, err)
		}
		hostPorts[i] = hostPort
	}

	// Verify all processes got different ports
	for i, hostPort := range hostPorts {
		for j, otherHostPort := range hostPorts {
			if i != j && hostPort == otherHostPort {
				t.Errorf("Processes should get different ports, but %s and %s both got %s",
					scripts[i], scripts[j], hostPort)
			}
		}
	}

	// Wait for servers to start
	time.Sleep(3 * time.Second)

	// Test that all servers are responding
	for i, hostPort := range hostPorts {
		resp, err := http.Get(fmt.Sprintf("http://%s/", hostPort))
		if err != nil {
			t.Errorf("Failed to connect to %s server: %v", scripts[i], err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 from %s, got %d", scripts[i], resp.StatusCode)
		}
	}
}

func TestE2E_ProcessReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	if !isDenoAvailable() {
		t.Skip("Deno not available, skipping E2E test")
	}

	transport := setupTestTransport(t)
	defer transport.Cleanup()

	scriptPath := getTestScript(t, "simple_server.js")

	// First call
	hostPort1, err := transport.manager.getOrCreateHost(scriptPath)
	if err != nil {
		t.Fatalf("Failed to get host:port first time: %v", err)
	}

	// Second call should reuse the same process
	hostPort2, err := transport.manager.getOrCreateHost(scriptPath)
	if err != nil {
		t.Fatalf("Failed to get host:port second time: %v", err)
	}

	if hostPort1 != hostPort2 {
		t.Errorf("Expected same host:port for same file, got %s != %s", hostPort1, hostPort2)
	}

	// Verify the process is marked as running
	transport.manager.mu.RLock()
	_, exists := transport.manager.processes[scriptPath]
	transport.manager.mu.RUnlock()

	if !exists {
		t.Error("Process should exist in manager")
	}

	// Process exists in manager, so it's running (if not running, it would be removed)
}

func TestE2E_SlowStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	if !isDenoAvailable() {
		t.Skip("Deno not available, skipping E2E test")
	}

	// Use shorter startup timeout for this test
	transport := &SubstrateTransport{
		IdleTimeout:    caddy.Duration(60 * time.Second),
		StartupTimeout: caddy.Duration(5 * time.Second), // Should be enough for 2s delay
	}

	ctx := caddy.Context{Context: context.Background()}
	err := transport.Provision(ctx)
	if err != nil {
		t.Fatalf("Failed to provision transport: %v", err)
	}
	defer transport.Cleanup()

	scriptPath := getTestScript(t, "slow_startup.js")

	start := time.Now()
	hostPort, err := transport.manager.getOrCreateHost(scriptPath)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to get host:port for slow startup: %v", err)
	}

	if duration < 2*time.Second {
		t.Errorf("Expected startup to take at least 2s, took %v", duration)
	}

	// Wait a bit more and verify server is accessible
	time.Sleep(1 * time.Second)
	resp, err := http.Get(fmt.Sprintf("http://%s/", hostPort))
	if err != nil {
		t.Fatalf("Failed to connect to slow startup server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestE2E_EchoServer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	if !isDenoAvailable() {
		t.Skip("Deno not available, skipping E2E test")
	}

	transport := setupTestTransport(t)
	defer transport.Cleanup()

	scriptPath := getTestScript(t, "echo_server.js")

	hostPort, err := transport.manager.getOrCreateHost(scriptPath)
	if err != nil {
		t.Fatalf("Failed to get host:port: %v", err)
	}

	// Wait for server to start
	time.Sleep(2 * time.Second)

	// Test POST request with body and headers
	reqBody := `{"test": "data"}`
	req, err := http.NewRequest("POST", fmt.Sprintf("http://%s/echo?param=value", hostPort), strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Header", "test-value")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	bodyStr := string(body)

	// Verify echo server captured our request details
	if !strings.Contains(bodyStr, `"method": "POST"`) {
		t.Errorf("Echo should capture POST method, response: %s", bodyStr)
	}

	if !strings.Contains(bodyStr, `"search": "?param=value"`) {
		t.Errorf("Echo should capture query params, response: %s", bodyStr)
	}

	if !strings.Contains(bodyStr, `"{\"test\": \"data\"}"`) {
		t.Errorf("Echo should capture request body, response: %s", bodyStr)
	}

	if !strings.Contains(bodyStr, `"x-test-header": "test-value"`) {
		t.Errorf("Echo should capture custom headers, response: %s", bodyStr)
	}
}

func TestE2E_ProcessCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	if !isDenoAvailable() {
		t.Skip("Deno not available, skipping E2E test")
	}

	transport := setupTestTransport(t)

	scriptPath := getTestScript(t, "graceful_shutdown.js")

	hostPort, err := transport.manager.getOrCreateHost(scriptPath)
	if err != nil {
		t.Fatalf("Failed to get host:port: %v", err)
	}

	// Wait for server to start
	time.Sleep(2 * time.Second)

	// Verify server is running
	resp, err := http.Get(fmt.Sprintf("http://%s/", hostPort))
	if err != nil {
		t.Fatalf("Failed to connect before cleanup: %v", err)
	}
	resp.Body.Close()

	// Get process count before cleanup
	transport.manager.mu.RLock()
	processesBefore := len(transport.manager.processes)
	transport.manager.mu.RUnlock()

	if processesBefore == 0 {
		t.Error("Expected at least one process before cleanup")
	}

	// Cleanup should stop all processes
	err = transport.Cleanup()
	if err != nil {
		t.Errorf("Cleanup should not return error: %v", err)
	}

	// Wait a moment for cleanup to complete
	time.Sleep(1 * time.Second)

	// Verify server is no longer accessible
	client := &http.Client{Timeout: 1 * time.Second}
	_, err = client.Get(fmt.Sprintf("http://%s/", hostPort))
	if err == nil {
		t.Error("Server should not be accessible after cleanup")
	}
}

func TestE2E_SymlinkExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	if !isDenoAvailable() {
		t.Skip("Deno not available, skipping E2E test")
	}

	transport := setupTestTransport(t)
	defer transport.Cleanup()

	// Get the original test script
	originalScript := getTestScript(t, "simple_server.js")

	// Create a temporary directory for the symlink
	tempDir, err := os.MkdirTemp("", "substrate-symlink-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

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

	// Wait for server to start
	time.Sleep(2 * time.Second)

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

func getTestScript(t *testing.T, filename string) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	scriptPath := filepath.Join(wd, "testdata", filename)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Fatalf("Test script not found: %s", scriptPath)
	}

	return scriptPath
}


