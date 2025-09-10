package e2e

import (
	"fmt"
	"testing"
)

func TestSimpleSubstrateRequest(t *testing.T) {
	caddyfile := `{
	{{GLOBAL}}
}

:{{HTTP_PORT}} {
	root {{TEMPDIR}}

	@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			idle_timeout 5m
			startup_timeout 5s
		}
		to localhost
	}
}`

	jsServer := `#!/usr/bin/env -S deno run --allow-net
const [host, port] = Deno.args;

if (!host || !port) {
  console.error("Usage: hello.js <host> <port>");
  Deno.exit(1);
}

const server = Deno.serve({
    hostname: host === "localhost" ? "127.0.0.1" : host,
    port: parseInt(port)
}, (req) => {
    return new Response("Hello from substrate process!\nURL: " + req.url, {
        headers: { "Content-Type": "text/plain" }
    });
});

console.log("Server running at http://" + host + ":" + port + "/");

Deno.addSignalListener("SIGTERM", () => {
    console.log("Received SIGTERM, shutting down gracefully");
    server.shutdown();
    Deno.exit(0);
});`

	files := []TestFile{
		{Path: "hello.js", Content: jsServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, caddyfile, files)
	defer ctx.TearDown()

	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/hello.js", 200, fmt.Sprintf("Hello from substrate process!\nURL: %s/hello.js", ctx.BaseURL))
}
