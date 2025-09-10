package e2e

import (
	"testing"
	"time"
)

func TestProcessRestartsAfterExit(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Server that exits after serving one request
	exitAfterOneServer := `#!/usr/bin/env -S deno run --allow-net
let requestCount = 0;
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	requestCount++;
	const response = new Response("Request #" + requestCount + " - goodbye!");
	
	// Exit after a very short delay to allow response to be sent
	setTimeout(() => {
		console.log("Server exiting after request", requestCount);
		Deno.exit(0);
	}, 10);
	
	return response;
});`

	files := []TestFile{
		{Path: "exit_server.js", Content: exitAfterOneServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// First request should work and cause the server to exit
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/exit_server.js", 200, "Request #1 - goodbye!")

	// Wait a moment for the process to exit
	time.Sleep(100 * time.Millisecond)

	// Second request should restart the process and get a new counter
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/exit_server.js", 200, "Request #1 - goodbye!")

	// Wait again and make a third request
	time.Sleep(100 * time.Millisecond)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/exit_server.js", 200, "Request #1 - goodbye!")
}

func TestProcessRestartAfterCrash(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Server that crashes after serving one request
	crashServer := `#!/usr/bin/env -S deno run --allow-net
let requestCount = 0;
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	requestCount++;
	const response = new Response("Request #" + requestCount + " before crash");
	
	// Crash after a very short delay
	setTimeout(() => {
		console.log("Server crashing after request", requestCount);
		Deno.exit(1); // Exit with error code
	}, 10);
	
	return response;
});`

	files := []TestFile{
		{Path: "crash_server.js", Content: crashServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// First request should work and cause the server to crash
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/crash_server.js", 200, "Request #1 before crash")

	// Wait for the process to crash and be cleaned up
	time.Sleep(100 * time.Millisecond)

	// Second request should restart the process
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/crash_server.js", 200, "Request #1 before crash")
}