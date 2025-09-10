package e2e

import (
	"fmt"
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

	jsServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Hello from substrate process!\nURL: " + req.url, {
    headers: { "Content-Type": "text/plain" }
  });
});`

	files := []TestFile{
		{Path: "hello.js", Content: jsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/hello.js", 200, fmt.Sprintf("Hello from substrate process!\nURL: %s/hello.js", ctx.BaseURL))
}

