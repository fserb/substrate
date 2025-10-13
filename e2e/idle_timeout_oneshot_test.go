package e2e

import (
	"testing"
	"time"
)

func TestIdleTimeoutOneShotMode(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			idle_timeout -1
		}
		to localhost
	}`

	// Server with a local counter that increments with each request
	jsServer := `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
const [socketPath] = Deno.args;

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

	ctx := RunE2ETest(t, serverBlock, files)

	// First request should return "Count: 1"
	ctx.AssertGet("/counter.js", "Count: 1")

	// Wait a moment for process to terminate
	time.Sleep(100 * time.Millisecond)

	// Second request should return "Count: 1" again (new process, counter reset)
	ctx.AssertGet("/counter.js", "Count: 1")
}