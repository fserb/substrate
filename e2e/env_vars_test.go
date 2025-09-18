package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestEnvironmentVariables(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			env {
				NODE_ENV production
				API_KEY secret123
				DEBUG_MODE true
				CUSTOM_VAR hello_world
			}
			idle_timeout 5m
		}
		to localhost
	}`

	envServer := `#!/usr/bin/env -S deno run --allow-net --allow-env
const [host, port] = Deno.args;

Deno.serve({hostname: host, port: parseInt(port)}, (req) => {
	const envVars = {
		NODE_ENV: Deno.env.get("NODE_ENV") || "not_set",
		API_KEY: Deno.env.get("API_KEY") || "not_set",
		DEBUG_MODE: Deno.env.get("DEBUG_MODE") || "not_set",
		CUSTOM_VAR: Deno.env.get("CUSTOM_VAR") || "not_set",
		PATH: Deno.env.get("PATH") ? "inherited" : "not_set" // Check parent env inheritance
	};

	return new Response(JSON.stringify(envVars), {
		headers: { "Content-Type": "application/json" }
	});
});`

	files := []TestFile{
		{Path: "env-test.js", Content: envServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make request to get the response body
	resp, err := http.Get(ctx.BaseURL + "/env-test.js")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	body := string(bodyBytes)

	var envVars map[string]string
	if err := json.Unmarshal([]byte(body), &envVars); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	// Verify configured environment variables
	expectedVars := map[string]string{
		"NODE_ENV":   "production",
		"API_KEY":    "secret123",
		"DEBUG_MODE": "true",
		"CUSTOM_VAR": "hello_world",
	}

	for key, expected := range expectedVars {
		if actual, exists := envVars[key]; !exists {
			t.Errorf("Environment variable %s is missing", key)
		} else if actual != expected {
			t.Errorf("Environment variable %s = %q, expected %q", key, actual, expected)
		}
	}

	// Verify parent environment is inherited (PATH should exist)
	if path, exists := envVars["PATH"]; !exists {
		t.Error("PATH environment variable should be inherited from parent")
	} else if path != "inherited" {
		t.Error("PATH environment variable should be inherited from parent")
	}
}

func TestEnvironmentVariablesMultipleProcesses(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			env {
				SHARED_VAR shared_value
				PROCESS_TYPE web_server
			}
		}
		to localhost
	}`

	// First process - returns SHARED_VAR and PROCESS_TYPE
	process1 := `#!/usr/bin/env -S deno run --allow-net --allow-env
const [host, port] = Deno.args;

Deno.serve({hostname: host, port: parseInt(port)}, (req) => {
	const response = {
		process: "process1",
		SHARED_VAR: Deno.env.get("SHARED_VAR") || "not_set",
		PROCESS_TYPE: Deno.env.get("PROCESS_TYPE") || "not_set"
	};
	return new Response(JSON.stringify(response), {
		headers: { "Content-Type": "application/json" }
	});
});`

	// Second process - also returns the same env vars
	process2 := `#!/usr/bin/env -S deno run --allow-net --allow-env
const [host, port] = Deno.args;

Deno.serve({hostname: host, port: parseInt(port)}, (req) => {
	const response = {
		process: "process2",
		SHARED_VAR: Deno.env.get("SHARED_VAR") || "not_set",
		PROCESS_TYPE: Deno.env.get("PROCESS_TYPE") || "not_set"
	};
	return new Response(JSON.stringify(response), {
		headers: { "Content-Type": "application/json" }
	});
});`

	files := []TestFile{
		{Path: "server1.js", Content: process1, Mode: 0755},
		{Path: "server2.js", Content: process2, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Test both processes get the same environment variables
	for _, path := range []string{"/server1.js", "/server2.js"} {
		resp, err := http.Get(ctx.BaseURL + path)
		if err != nil {
			t.Fatalf("Failed to make request to %s: %v", path, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("Expected status 200 for %s, got %d", path, resp.StatusCode)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body for %s: %v", path, err)
		}
		body := string(bodyBytes)

		var response map[string]string
		if err := json.Unmarshal([]byte(body), &response); err != nil {
			t.Fatalf("Failed to parse JSON response for %s: %v", path, err)
		}

		// Both processes should have the same env vars
		if response["SHARED_VAR"] != "shared_value" {
			t.Errorf("Process %s: SHARED_VAR = %q, expected %q",
				response["process"], response["SHARED_VAR"], "shared_value")
		}

		if response["PROCESS_TYPE"] != "web_server" {
			t.Errorf("Process %s: PROCESS_TYPE = %q, expected %q",
				response["process"], response["PROCESS_TYPE"], "web_server")
		}
	}
}

func TestEnvironmentVariablesEmpty(t *testing.T) {
	// Test that substrate works when no env vars are configured
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	envServer := `#!/usr/bin/env -S deno run --allow-net --allow-env
const [host, port] = Deno.args;

Deno.serve({hostname: host, port: parseInt(port)}, (req) => {
	// Should still inherit parent environment
	const hasPath = Deno.env.get("PATH") ? true : false;

	return new Response(JSON.stringify({
		has_path: hasPath,
		custom_var: Deno.env.get("CUSTOM_VAR") || "not_set"
	}), {
		headers: { "Content-Type": "application/json" }
	});
});`

	files := []TestFile{
		{Path: "no-env.js", Content: envServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	resp, err := http.Get(ctx.BaseURL + "/no-env.js")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	body := string(bodyBytes)

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	// Should inherit parent environment
	if hasPath, ok := response["has_path"].(bool); !ok || !hasPath {
		t.Error("Process should inherit PATH from parent environment")
	}

	// Custom var should not be set
	if customVar, ok := response["custom_var"].(string); !ok || customVar != "not_set" {
		t.Errorf("CUSTOM_VAR should be 'not_set', got %q", customVar)
	}
}