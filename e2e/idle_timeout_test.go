package e2e

import (
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestOneShotModeStateReset verifies that one-shot mode (idle_timeout -1)
// starts a fresh process for each request, with no state carried over.
func TestOneShotModeStateReset(t *testing.T) {
	// Server with a local counter that increments with each request
	jsServer := `const [socketPath] = Deno.args;

let count = 0;

Deno.serve({
	path: socketPath
}, (req) => {
	count++;
	return new Response("Count: " + count, {
		headers: { "Content-Type": "text/plain" }
	});
});`

	files := []TestFile{
		{Path: "counter.js", Content: jsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, ServerBlockWithConfig(SubstrateConfig{IdleTimeout: "-1"}), files)

	// First request should return "Count: 1"
	ctx.AssertGet("/counter.js", "Count: 1")

	// Wait a moment for process to terminate
	time.Sleep(100 * time.Millisecond)

	// Second request should return "Count: 1" again (new process, counter reset)
	ctx.AssertGet("/counter.js", "Count: 1")
}

// TestOneShotModeProcessCleanup verifies that one-shot mode actually terminates
// processes after each request, confirmed via PID changes and ps checks.
func TestOneShotModeProcessCleanup(t *testing.T) {
	// Server that logs its PID so we can track it
	jsServer := `const [socketPath] = Deno.args;

Deno.serve({
	path: socketPath
}, (req) => {
	return new Response("PID: " + Deno.pid, {
		headers: { "Content-Type": "text/plain" }
	});
});`

	files := []TestFile{
		{Path: "server.js", Content: jsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, ServerBlockWithConfig(SubstrateConfig{IdleTimeout: "-1"}), files)

	// First request
	resp1, err := ctx.Tester.Client.Get(ctx.BaseURL + "/server.js")
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if !strings.Contains(string(body1), "PID:") {
		t.Fatalf("Expected response to contain 'PID:', got: %s", string(body1))
	}

	pid1 := strings.TrimSpace(strings.TrimPrefix(string(body1), "PID:"))
	t.Logf("First request PID: %s", pid1)

	// Wait for process to be killed
	time.Sleep(500 * time.Millisecond)

	// Check if first process is still running
	checkCmd := exec.Command("ps", "-p", pid1)
	if checkCmd.Run() == nil {
		t.Errorf("Process %s is still running after one-shot request completed", pid1)
	}

	// Second request should spawn a new process
	resp2, err := ctx.Tester.Client.Get(ctx.BaseURL + "/server.js")
	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()

	if !strings.Contains(string(body2), "PID:") {
		t.Fatalf("Expected response to contain 'PID:', got: %s", string(body2))
	}

	pid2 := strings.TrimSpace(strings.TrimPrefix(string(body2), "PID:"))
	t.Logf("Second request PID: %s", pid2)

	if pid1 == pid2 {
		t.Errorf("Expected different PIDs, got same PID %s for both requests", pid1)
	}

	// Wait for second process to be killed
	time.Sleep(500 * time.Millisecond)

	// Check if second process is still running
	checkCmd = exec.Command("ps", "-p", pid2)
	if checkCmd.Run() == nil {
		t.Errorf("Process %s is still running after one-shot request completed", pid2)
	}
}
