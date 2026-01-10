package e2e

import (
	"testing"
)

// TestNonExecutableFilesWork verifies that files without executable permission
// can still be run via Deno. Unlike direct execution, Deno reads the file content
// so executable permission is not required.
func TestNonExecutableFilesWork(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	// Note: No shebang needed since we run via deno
	executableServer := `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("Executable server response");
});`

	// Same content, but without executable permission
	nonExecutableServer := `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("Non-executable server response");
});`

	files := []TestFile{
		{Path: "executable.js", Content: executableServer, Mode: 0755},
		{Path: "non_executable.js", Content: nonExecutableServer, Mode: 0644},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Both should work since Deno runs the files (not the OS)
	ctx.AssertGet("/executable.js", "Executable server response")
	ctx.AssertGet("/non_executable.js", "Non-executable server response")
}

// TestReadOnlyFileWorks verifies that read-only files can be run via Deno
func TestReadOnlyFileWorks(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	readOnlyServer := `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("Read-only server response");
});`

	files := []TestFile{
		{Path: "readonly.js", Content: readOnlyServer, Mode: 0444},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Should work since Deno reads the file (doesn't need execute permission)
	ctx.AssertGet("/readonly.js", "Read-only server response")
}
