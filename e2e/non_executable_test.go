package e2e

import (
	"testing"
)

func TestNonExecutableFilesReturnError(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Valid executable server
	executableServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Executable server response");
});`

	// Non-executable file (same content but no execute permission)
	nonExecutableServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("This should not run");
});`

	files := []TestFile{
		{Path: "executable.js", Content: executableServer, Mode: 0755},
		{Path: "non_executable.js", Content: nonExecutableServer, Mode: 0644}, // No execute permission
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// Test that executable file works
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/executable.js", 200, "Executable server response")

	// Test that non-executable file returns 502 error with empty body
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/non_executable.js", 502, "")
}

func TestReadOnlyFileReturnError(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	readOnlyServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("This should not run");
});`

	files := []TestFile{
		{Path: "readonly.js", Content: readOnlyServer, Mode: 0444}, // Read-only, not executable
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// Test that read-only file returns 502 error with empty body
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/readonly.js", 502, "")
}