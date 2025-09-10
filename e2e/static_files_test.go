package e2e

import (
	"testing"
)

func TestStaticFilesAreServedNormally(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}

	file_server`

	jsServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Dynamic JS response", {
		headers: { "Content-Type": "text/plain" }
	});
});`

	staticHTML := `<!DOCTYPE html>
<html>
<head><title>Static HTML</title></head>
<body><h1>This is static content</h1></body>
</html>`

	staticText := `This is a static text file.
It should be served directly by Caddy without going through substrate.`

	files := []TestFile{
		{Path: "app.js", Content: jsServer, Mode: 0755},
		{Path: "index.html", Content: staticHTML, Mode: 0644},
		{Path: "readme.txt", Content: staticText, Mode: 0644},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	ctx.AssertGet("/app.js", "Dynamic JS response")

	ctx.AssertGet("/index.html", staticHTML)

	ctx.AssertGet("/readme.txt", staticText)
}

func TestOnlyMatchedFilesAreProxied(t *testing.T) {
	serverBlock := `@app_files {
		path *.app.js
		file {path}
	}

	reverse_proxy @app_files {
		transport substrate
		to localhost
	}

	file_server`

	appServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("App server response");
});`

	regularJS := `// This is just a regular JS file
console.log("Hello World");`

	files := []TestFile{
		{Path: "main.app.js", Content: appServer, Mode: 0755},
		{Path: "regular.js", Content: regularJS, Mode: 0644},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	ctx.AssertGet("/main.app.js", "App server response")

	ctx.AssertGet("/regular.js", regularJS)
}
