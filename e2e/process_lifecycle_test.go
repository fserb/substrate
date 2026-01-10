package e2e

import (
	"fmt"
	"testing"
	"time"
)

// TestProcessReusesForMultipleRequests verifies that multiple requests to the same file
// reuse the same process (counter increments across requests).
func TestProcessReusesForMultipleRequests(t *testing.T) {
	counterServer := `let count = 0;
Deno.serve({path: Deno.args[0]}, (req) => {
	count++;
	return new Response("Request #" + count);
});`

	files := []TestFile{
		{Path: "counter.js", Content: counterServer},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

	ctx.AssertGet("/counter.js", "Request #1")

	time.Sleep(10 * time.Millisecond)
	ctx.AssertGet("/counter.js", "Request #2")

	time.Sleep(10 * time.Millisecond)
	ctx.AssertGet("/counter.js", "Request #3")

	time.Sleep(10 * time.Millisecond)
	ctx.AssertGet("/counter.js", "Request #4")
}

// TestDifferentFilesGetDifferentProcesses verifies that different files get separate
// processes with independent counters.
func TestDifferentFilesGetDifferentProcesses(t *testing.T) {
	serverTemplate := `const filename = "%s";
let count = 0;
Deno.serve({path: Deno.args[0]}, (req) => {
	count++;
	return new Response(filename + " request #" + count);
});`

	files := []TestFile{
		{Path: "server1.js", Content: fmt.Sprintf(serverTemplate, "server1")},
		{Path: "server2.js", Content: fmt.Sprintf(serverTemplate, "server2")},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

	ctx.AssertGet("/server1.js", "server1 request #1")
	ctx.AssertGet("/server2.js", "server2 request #1")
	ctx.AssertGet("/server1.js", "server1 request #2")
	ctx.AssertGet("/server2.js", "server2 request #2")
}

// TestProcessRestartsAfterExit verifies that a process is automatically restarted
// after it exits cleanly (exit code 0). The counter should reset to 1 after restart.
func TestProcessRestartsAfterExit(t *testing.T) {
	exitAfterOneServer := `let requestCount = 0;
Deno.serve({path: Deno.args[0]}, (req) => {
	requestCount++;
	const response = new Response("Request #" + requestCount + " - goodbye!");

	setTimeout(() => {
		console.log("Server exiting after request", requestCount);
		Deno.exit(0);
	}, 10);

	return response;
});`

	files := []TestFile{
		{Path: "exit_server.js", Content: exitAfterOneServer},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

	ctx.AssertGet("/exit_server.js", "Request #1 - goodbye!")

	time.Sleep(100 * time.Millisecond)

	ctx.AssertGet("/exit_server.js", "Request #1 - goodbye!")

	time.Sleep(100 * time.Millisecond)
	ctx.AssertGet("/exit_server.js", "Request #1 - goodbye!")
}

// TestProcessRestartAfterCrash verifies that a process is automatically restarted
// after it crashes (exit code 1). The counter should reset to 1 after restart.
func TestProcessRestartAfterCrash(t *testing.T) {
	crashServer := `let requestCount = 0;
Deno.serve({path: Deno.args[0]}, (req) => {
	requestCount++;
	const response = new Response("Request #" + requestCount + " before crash");

	setTimeout(() => {
		console.log("Server crashing after request", requestCount);
		Deno.exit(1);
	}, 10);

	return response;
});`

	files := []TestFile{
		{Path: "crash_server.js", Content: crashServer},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

	ctx.AssertGet("/crash_server.js", "Request #1 before crash")

	time.Sleep(100 * time.Millisecond)

	ctx.AssertGet("/crash_server.js", "Request #1 before crash")
}
