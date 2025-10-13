package e2e

import (
	"testing"
	"time"
)

func TestProcessStdoutLogging(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			idle_timeout 1m
			startup_timeout 10s
		}
		to localhost
	}`

	files := []TestFile{
		{
			Path: "stdout_test.js",
			Content: `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
const [socketPath] = Deno.args;

// Log some messages to stdout and stderr
console.log("Starting server on " + socketPath);
console.error("This is an error message");
console.log("Server ready");

const server = Deno.serve({
  path: socketPath
}, (req) => {
  console.log("Handling request to: " + req.url);
  return new Response("Hello from stdout test!");
});

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Shutting down server");
  server.shutdown();
  Deno.exit(0);
});
`,
			Mode: 0755,
		},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make a request to trigger process startup and logging
	ctx.AssertGet("/stdout_test.js", "Hello from stdout test!")

	// Give some time for logs to be processed
	time.Sleep(100 * time.Millisecond)

	// Make another request to trigger more logging
	ctx.AssertGet("/stdout_test.js", "Hello from stdout test!")
}

func TestProcessStderrLogging(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			idle_timeout 1m
			startup_timeout 10s
		}
		to localhost
	}`

	files := []TestFile{
		{
			Path: "stderr_test.js",
			Content: `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
const [socketPath] = Deno.args;

// Log messages to both stdout and stderr
console.log("Starting server on " + socketPath);
console.error("Warning: This is a test warning");
console.log("Server initialization complete");

const server = Deno.serve({path: socketPath}, (req) => {
	const url = new URL(req.url);
	console.log("Processing request: " + url.pathname);
	console.error("Debug: Request received");
	return new Response("Hello from stderr test!");
});

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
	console.error("Received shutdown signal");
	console.log("Shutting down server");
	server.shutdown();
	Deno.exit(0);
});
`,
			Mode: 0755,
		},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make a request to trigger process startup and logging
	ctx.AssertGet("/stderr_test.js", "Hello from stderr test!")

	// Give some time for logs to be processed
	time.Sleep(100 * time.Millisecond)

	// Make another request to trigger more logging
	ctx.AssertGet("/stderr_test.js", "Hello from stderr test!")
}

func TestProcessOutputWithCrash(t *testing.T) {
	serverBlock := `@crash_files {
		path *.sh
		file {path}
	}

	reverse_proxy @crash_files {
		transport substrate {
			idle_timeout 1m
			startup_timeout 5s
		}
		to localhost
	}`

	files := []TestFile{
		{
			Path: "crash_test.sh",
			Content: `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
const [socketPath] = Deno.args;

console.log("Starting crash test server on " + socketPath);
console.error("This server will crash after starting");

let requestCount = 0;

Deno.serve({path: socketPath}, (req) => {
	requestCount++;
	console.log("Handling request before crash");
	console.error("About to crash!");

	if (requestCount === 1) {
		// Crash after first request
		setTimeout(() => {
			console.error("Crashing now!");
			Deno.exit(1);
		}, 10);
	}

	return new Response("Response before crash");
});
`,
			Mode: 0755,
		},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make a request that should cause the process to crash
	ctx.AssertGet("/crash_test.sh", "Response before crash")

	// Give time for crash logging
	time.Sleep(200 * time.Millisecond)

	// Second request should create a new process
	ctx.AssertGet("/crash_test.sh", "Response before crash")
}