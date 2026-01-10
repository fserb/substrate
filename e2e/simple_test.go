package e2e

import (
	"testing"
)

func TestSimpleSubstrateRequest(t *testing.T) {
	jsServer := `Deno.serve({path: Deno.args[0]}, (req) => {
	const url = new URL(req.url);
	return new Response("Hello from substrate process!\nPath: " + url.pathname, {
    headers: { "Content-Type": "text/plain" }
  });
});`

	files := []TestFile{
		{Path: "hello.js", Content: jsServer},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

	ctx.AssertGet("/hello.js", "Hello from substrate process!\nPath: /hello.js")
}
