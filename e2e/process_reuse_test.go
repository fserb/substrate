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

	ctx.AssertGet("/counter.js", "Request #1")

	time.Sleep(10 * time.Millisecond)
	ctx.AssertGet("/counter.js", "Request #2")

	time.Sleep(10 * time.Millisecond)
	ctx.AssertGet("/counter.js", "Request #3")

	time.Sleep(10 * time.Millisecond)
	ctx.AssertGet("/counter.js", "Request #4")
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

	ctx.AssertGet("/server1.js", "server1 request #1")
	ctx.AssertGet("/server2.js", "server2 request #1")
	ctx.AssertGet("/server1.js", "server1 request #2")
	ctx.AssertGet("/server2.js", "server2 request #2")
}
