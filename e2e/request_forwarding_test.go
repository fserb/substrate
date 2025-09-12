package e2e

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestRequestHeadersAreForwarded(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Server that echoes back request headers
	headerEchoServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	const headers = {};
	for (const [key, value] of req.headers.entries()) {
		headers[key] = value;
	}

	return new Response(JSON.stringify({
		method: req.method,
		url: req.url,
		headers: headers
	}, null, 2), {
		headers: { "Content-Type": "application/json" }
	});
});`

	files := []TestFile{
		{Path: "headers.js", Content: headerEchoServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Create request with custom headers
	req, err := http.NewRequest("GET", ctx.BaseURL+"/headers.js", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("X-Custom-Header", "test-value")
	req.Header.Set("User-Agent", "substrate-test/1.0")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	body := make([]byte, 2048)
	n, _ := resp.Body.Read(body)
	responseBody := string(body[:n])

	// Verify headers were forwarded (headers are case-insensitive, normalized to lowercase)
	if !strings.Contains(strings.ToLower(responseBody), "x-custom-header") {
		t.Error("Custom header not found in response")
	}
	if !strings.Contains(responseBody, "test-value") {
		t.Error("Custom header value not found in response")
	}
	if !strings.Contains(responseBody, "substrate-test/1.0") {
		t.Error("User-Agent not found in response")
	}

	t.Logf("Headers response: %s", responseBody)
}

func TestRequestBodyIsForwarded(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Server that echoes back request body
	bodyEchoServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, async (req) => {
	const body = await req.text();

	return new Response(JSON.stringify({
		method: req.method,
		url: req.url,
		contentType: req.headers.get("content-type"),
		bodyLength: body.length,
		body: body
	}, null, 2), {
		headers: { "Content-Type": "application/json" }
	});
});`

	files := []TestFile{
		{Path: "body.js", Content: bodyEchoServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Test POST request with JSON body
	jsonData := `{"message": "Hello from substrate test", "number": 42}`
	req, err := http.NewRequest("POST", ctx.BaseURL+"/body.js", bytes.NewBufferString(jsonData))
	if err != nil {
		t.Fatalf("Failed to create POST request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	body := make([]byte, 2048)
	n, _ := resp.Body.Read(body)
	responseBody := string(body[:n])

	// Verify body was forwarded
	if !strings.Contains(responseBody, "Hello from substrate test") {
		t.Error("Request body not found in response")
	}
	if !strings.Contains(responseBody, "application/json") {
		t.Error("Content-Type not found in response")
	}
	if !strings.Contains(responseBody, "POST") {
		t.Error("Method not found in response")
	}

	t.Logf("Body response: %s", responseBody)
}

func TestQueryParametersAreForwarded(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Server that echoes back URL and query parameters
	queryEchoServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	const url = new URL(req.url);
	const params = {};

	for (const [key, value] of url.searchParams.entries()) {
		params[key] = value;
	}

	return new Response(JSON.stringify({
		url: req.url,
		pathname: url.pathname,
		search: url.search,
		params: params
	}, null, 2), {
		headers: { "Content-Type": "application/json" }
	});
});`

	files := []TestFile{
		{Path: "query.js", Content: queryEchoServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make request with query parameters
	url := ctx.BaseURL + "/query.js?name=substrate&version=3&test=true"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Request with query params failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	body := make([]byte, 2048)
	n, _ := resp.Body.Read(body)
	responseBody := string(body[:n])

	// Verify query parameters were forwarded
	if !strings.Contains(responseBody, "name") {
		t.Error("Query parameter 'name' not found in response")
	}
	if !strings.Contains(responseBody, "substrate") {
		t.Error("Query parameter value 'substrate' not found in response")
	}
	if !strings.Contains(responseBody, "version") {
		t.Error("Query parameter 'version' not found in response")
	}
	if !strings.Contains(responseBody, "?name=substrate&version=3&test=true") {
		t.Error("Complete query string not found in response")
	}

	t.Logf("Query response: %s", responseBody)
}
