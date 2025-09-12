package e2e

import (
	"testing"
)

func TestURLRewriting(t *testing.T) {
	// Caddyfile block that matches "x" and rewrites to "x.js"
	serverBlock := `@simple_rewrite {
		not path *.js
		file {path}.js
	}

	reverse_proxy @simple_rewrite {
		transport substrate
		to localhost
	}

	file_server`

	pathEchoServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	const url = new URL(req.url);
	return new Response(url.pathname);
});`

	files := []TestFile{
		{Path: "echo.js", Content: pathEchoServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGet("/echo", "/echo")
	ctx.AssertGet("/echo.js", pathEchoServer)
}
