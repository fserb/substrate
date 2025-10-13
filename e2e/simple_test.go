package e2e

import (
	"testing"
)

func TestSimpleSubstrateRequest(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	jsServer := `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
Deno.serve({path: Deno.args[0]}, (req) => {
	const url = new URL(req.url);
	return new Response("Hello from substrate process!\nPath: " + url.pathname, {
    headers: { "Content-Type": "text/plain" }
  });
});`

	files := []TestFile{
		{Path: "hello.js", Content: jsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGet("/hello.js", "Hello from substrate process!\nPath: /hello.js")
}
