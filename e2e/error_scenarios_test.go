package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMissingFileReturnsError(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}

	file_server`

	files := []TestFile{}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/nonexistent.js", 404)
}

func TestSlowStartupHandling(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			startup_timeout 1s
		}
		to localhost
	}`

	slowStartupServer := `console.log("Starting slow server...");

// Simulate slow startup
await new Promise(resolve => setTimeout(resolve, 300));

console.log("Slow server ready");
Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("Slow server finally responded!");
});`

	files := []TestFile{
		{Path: "slow.js", Content: slowStartupServer},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	start := time.Now()
	ctx.AssertGet("/slow.js", "Slow server finally responded!")
	duration := time.Since(start)

	if duration < 300*time.Millisecond {
		t.Errorf("Request completed too quickly: %v", duration)
	}

	t.Logf("Slow startup request took: %v", duration)
}

func TestVerySlowStartupTimeout(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			startup_timeout 200ms
		}
		to localhost
	}`

	verySlowServer := `console.log("Starting very slow server...");

// Simulate very slow startup (longer than timeout)
await new Promise(resolve => setTimeout(resolve, 500));

console.log("Very slow server ready");
Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("This should timeout");
});`

	files := []TestFile{
		{Path: "very_slow.js", Content: verySlowServer},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/very_slow.js", 502)
}

func TestServerThatFailsToStart(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	failingServer := `// This will cause a syntax error
this is not valid javascript code!!!
Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("This won't work");
});`

	files := []TestFile{
		{Path: "failing.js", Content: failingServer},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/failing.js", 502)
}

func TestServerThatBindsToWrongPort(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	wrongPortServer := `console.log("Args:", Deno.args);
// Ignore the provided socket and use a different one
Deno.serve({path: "/tmp/wrong-socket.sock"}, (req) => {
	return new Response("Wrong socket server");
});`

	files := []TestFile{
		{Path: "wrong_port.js", Content: wrongPortServer},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/wrong_port.js", 502)
}

func TestDetailedErrorForInternalIP(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			startup_timeout 500ms
		}
		to localhost
	}`

	// Create a script that will fail to start
	failingScript := `console.log("Starting failing script...");
console.error("This will be captured in stderr");
// This will cause a syntax error
this is not valid javascript syntax!!!
`

	files := []TestFile{
		{Path: "failing.js", Content: failingScript},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make a request - this should get detailed error info since it's from localhost (internal)
	resp, err := http.Get(ctx.BaseURL + "/failing.js")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Errorf("Expected status 502, got %d", resp.StatusCode)
	}

	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	responseBody := string(body[:n])

	t.Logf("Response body: %s", responseBody)

	// Since this is from localhost (internal IP), we should get detailed error info
	expectedStrings := []string{
		"Process startup failed",
		"Exit code: 1",
		"Stderr:",
		"syntax",
		"not valid javascript",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(responseBody, expected) {
			t.Errorf("Expected response body to contain %q, but it didn't.\nFull body: %s", expected, responseBody)
		}
	}

	// Should be text/plain; charset=utf-8
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/plain; charset=utf-8" {
		t.Errorf("Expected Content-Type: text/plain; charset=utf-8, got %q", contentType)
	}
}

func TestProcessStartupTimeoutWithDetailedError(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			startup_timeout 100ms
		}
		to localhost
	}`

	// Create a script that takes too long to start
	slowScript := `console.log("Starting slow script...");
console.error("This is stderr output");
// Wait longer than the timeout
await new Promise(resolve => setTimeout(resolve, 200));
console.log("Finally starting server (but this will be too late)");
Deno.serve({path: Deno.args[0]}, () => {
	return new Response("Too slow!");
});
`

	files := []TestFile{
		{Path: "slow.js", Content: slowScript},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	resp, err := http.Get(ctx.BaseURL + "/slow.js")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 502 {
		t.Errorf("Expected status 502, got %d", resp.StatusCode)
	}

	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	responseBody := string(body[:n])

	t.Logf("Response body: %s", responseBody)

	// Should contain timeout-related error
	expectedStrings := []string{
		"Process startup failed",
		"timeout",
		"Exit code: -1", // Process was terminated by SIGTERM (signal termination returns -1)
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(responseBody, expected) {
			t.Errorf("Expected response body to contain %q, but it didn't.\nFull body: %s", expected, responseBody)
		}
	}
}
