package e2e

import (
	"fmt"
	"testing"
	"time"
)

func TestProcessReusesForMultipleRequests(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Server that counts requests to verify same process is reused
	counterServer := `#!/usr/bin/env -S deno run --allow-net
let count = 0;
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	count++;
	return new Response("Request #" + count);
});`

	files := []TestFile{
		{Path: "counter.js", Content: counterServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// Make multiple requests and verify the counter increments
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/counter.js", 200, "Request #1")
	
	// Small delay to ensure requests are sequential
	time.Sleep(10 * time.Millisecond)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/counter.js", 200, "Request #2")
	
	time.Sleep(10 * time.Millisecond)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/counter.js", 200, "Request #3")
	
	time.Sleep(10 * time.Millisecond)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/counter.js", 200, "Request #4")
}

func TestDifferentFilesGetDifferentProcesses(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Server that reports its process info
	serverTemplate := `#!/usr/bin/env -S deno run --allow-net
const filename = "%s";
let count = 0;
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	count++;
	return new Response(filename + " request #" + count);
});`

	files := []TestFile{
		{Path: "server1.js", Content: fmt.Sprintf(serverTemplate, "server1"), Mode: 0755},
		{Path: "server2.js", Content: fmt.Sprintf(serverTemplate, "server2"), Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// Test that each file gets its own process with independent counters
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/server1.js", 200, "server1 request #1")
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/server2.js", 200, "server2 request #1")
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/server1.js", 200, "server1 request #2")
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/server2.js", 200, "server2 request #2")
}