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

	exitAfterOneServer := `#!/usr/bin/env -S deno run --allow-net
let requestCount = 0;
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	requestCount++;
	const response = new Response("Request #" + requestCount + " - goodbye!");
	
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

	ctx.AssertGet("/exit_server.js", "Request #1 - goodbye!")

	time.Sleep(100 * time.Millisecond)

	ctx.AssertGet("/exit_server.js", "Request #1 - goodbye!")

	time.Sleep(100 * time.Millisecond)
	ctx.AssertGet("/exit_server.js", "Request #1 - goodbye!")
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

	crashServer := `#!/usr/bin/env -S deno run --allow-net
let requestCount = 0;
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	requestCount++;
	const response = new Response("Request #" + requestCount + " before crash");
	
	setTimeout(() => {
		console.log("Server crashing after request", requestCount);
		Deno.exit(1);
	}, 10);
	
	return response;
});`

	files := []TestFile{
		{Path: "crash_server.js", Content: crashServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	ctx.AssertGet("/crash_server.js", "Request #1 before crash")

	time.Sleep(100 * time.Millisecond)

	ctx.AssertGet("/crash_server.js", "Request #1 before crash")
}
