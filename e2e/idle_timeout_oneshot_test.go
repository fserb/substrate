package e2e

import (
	"testing"
	"time"
)

func TestIdleTimeoutOneShotMode(t *testing.T) {
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
