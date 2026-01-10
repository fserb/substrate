package e2e

import (
	"testing"
)

// TestFilePermissionsDontMatter verifies that Deno can run files regardless of
// their executable permission. Unlike direct execution, Deno reads the file content
// so execute permission is not required.
func TestFilePermissionsDontMatter(t *testing.T) {
	files := []TestFile{
		{Path: "executable.js", Content: `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("0755 response");
});`, Mode: 0755},
		{Path: "regular.js", Content: `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("0644 response");
});`, Mode: 0644},
		{Path: "readonly.js", Content: `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("0444 response");
});`, Mode: 0444},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

	// All should work since Deno reads files (doesn't need execute permission)
	ctx.AssertGet("/executable.js", "0755 response")
	ctx.AssertGet("/regular.js", "0644 response")
	ctx.AssertGet("/readonly.js", "0444 response")
}
