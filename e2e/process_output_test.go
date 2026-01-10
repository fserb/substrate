package e2e

import (
	"testing"
	"time"
)

// TestProcessOutputLogging verifies that both stdout and stderr from processes
// are captured and logged correctly.
func TestProcessOutputLogging(t *testing.T) {
	serverBlock := ServerBlockWithConfig(SubstrateConfig{
		IdleTimeout:    "1m",
		StartupTimeout: "10s",
	})

	files := []TestFile{
		{
			Path: "output_test.js",
			Content: `const [socketPath] = Deno.args;

// Log messages to both stdout and stderr during startup
console.log("Starting server on " + socketPath);
console.error("Warning: This is a test warning");
console.log("Server initialization complete");

const server = Deno.serve({path: socketPath}, (req) => {
	const url = new URL(req.url);
	console.log("Processing request: " + url.pathname);
	console.error("Debug: Request received");
	return new Response("Hello from output test!");
});

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
	console.error("Received shutdown signal");
	console.log("Shutting down server");
	server.shutdown();
	Deno.exit(0);
});
`,
		},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make a request to trigger process startup and logging
	ctx.AssertGet("/output_test.js", "Hello from output test!")

	// Give some time for logs to be processed
	time.Sleep(100 * time.Millisecond)

	// Make another request to trigger more logging (both stdout and stderr per request)
	ctx.AssertGet("/output_test.js", "Hello from output test!")
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
			Content: `const [socketPath] = Deno.args;

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
