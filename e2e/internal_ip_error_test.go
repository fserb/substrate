package e2e

import (
	"net/http"
	"strings"
	"testing"
)

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
	failingScript := `#!/usr/bin/env -S deno run --allow-net
console.log("Starting failing script...");
console.error("This will be captured in stderr");
// This will cause a syntax error
this is not valid javascript syntax!!!
`

	files := []TestFile{
		{Path: "failing.js", Content: failingScript, Mode: 0755},
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

	// Should be text/plain
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/plain" {
		t.Errorf("Expected Content-Type: text/plain, got %q", contentType)
	}
}

func TestProcessStartupTimeout(t *testing.T) {
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
	slowScript := `#!/usr/bin/env -S deno run --allow-net
console.log("Starting slow script...");
console.error("This is stderr output");
// Wait longer than the timeout
await new Promise(resolve => setTimeout(resolve, 200));
console.log("Finally starting server (but this will be too late)");
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, () => {
	return new Response("Too slow!");
});
`

	files := []TestFile{
		{Path: "slow.js", Content: slowScript, Mode: 0755},
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
		"Exit code: 0", // Process exists but isn't bound to port yet
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(responseBody, expected) {
			t.Errorf("Expected response body to contain %q, but it didn't.\nFull body: %s", expected, responseBody)
		}
	}
}

