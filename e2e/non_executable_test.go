package e2e

import (
	"testing"
)

// TestFilePermissionsDontMatter verifies that Deno can run files regardless of
// their executable permission. Unlike direct execution, Deno reads the file content
// so execute permission is not required.
func TestFilePermissionsDontMatter(t *testing.T) {
	files := []TestFile{
		{Path: "default.js", Content: `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("default (0644) response");
});`},
		{Path: "readonly.js", Content: `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("0444 response");
});`, Mode: 0444},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

	// All should work since Deno reads files (doesn't need execute permission)
	ctx.AssertGet("/default.js", "default (0644) response")
	ctx.AssertGet("/readonly.js", "0444 response")
}
