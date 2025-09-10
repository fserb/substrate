package e2e

import (
	"testing"
)

func TestMixedStaticAndDynamicContent(t *testing.T) {
	serverBlock := `@api_files {
		path /api/*.js
		file {path}
	}

	reverse_proxy @api_files {
		transport substrate
		to localhost
	}

	file_server`

	dynamicServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Dynamic content from: " + req.url);
});`

	indexHTML := `<!DOCTYPE html>
<html>
<head><title>Mixed Content Test</title></head>
<body>
	<h1>Static HTML Page</h1>
	<p>This page contains both static and dynamic content.</p>
</body>
</html>`

	styleCSS := `body {
	font-family: Arial, sans-serif;
	margin: 20px;
	background-color: #f0f0f0;
}

h1 {
	color: #333;
	border-bottom: 2px solid #007acc;
}`

	clientJS := `// Client-side JavaScript
document.addEventListener('DOMContentLoaded', function() {
	console.log('Static JavaScript loaded');
	
	// This is client-side JS, not a server
	const message = 'Hello from static JS file';
	console.log(message);
});`

	files := []TestFile{
		{Path: "index.html", Content: indexHTML, Mode: 0644},
		{Path: "style.css", Content: styleCSS, Mode: 0644},
		{Path: "client.js", Content: clientJS, Mode: 0644},          // Not executable
		{Path: "api/server.js", Content: dynamicServer, Mode: 0755}, // Executable - becomes server
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	ctx.AssertGet("/index.html", indexHTML)

	ctx.AssertGet("/style.css", styleCSS)

	ctx.AssertGet("/client.js", clientJS)

	ctx.AssertGet("/api/server.js", "Dynamic content from: "+ctx.BaseURL+"/api/server.js")
}

func TestDifferentFileExtensions(t *testing.T) {
	serverBlock := `@python_files {
		path *.py
		file {path}
	}
	
	@shell_scripts {
		path *.sh
		file {path}
	}

	reverse_proxy @python_files {
		transport substrate
		to localhost
	}
	
	reverse_proxy @shell_scripts {
		transport substrate
		to localhost
	}

	file_server`

	pythonServer := `#!/usr/bin/env -S deno run --allow-net
// Simulating Python server
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Python-like server response from: " + req.url);
});`

	shellServer := `#!/usr/bin/env -S deno run --allow-net
// Simulating shell script server
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Shell-like server response from: " + req.url);
});`

	readmeTxt := `# Project README

This is a static text file that should be served directly.

It contains information about the project.`

	configJSON := `{
	"name": "substrate-test",
	"version": "3.0.0",
	"description": "Test configuration file",
	"static": true
}`

	files := []TestFile{
		{Path: "server.py", Content: pythonServer, Mode: 0755},
		{Path: "script.sh", Content: shellServer, Mode: 0755},
		{Path: "README.txt", Content: readmeTxt, Mode: 0644},
		{Path: "config.json", Content: configJSON, Mode: 0644},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	ctx.AssertGet("/server.py", "Python-like server response from: "+ctx.BaseURL+"/server.py")

	ctx.AssertGet("/script.sh", "Shell-like server response from: "+ctx.BaseURL+"/script.sh")

	ctx.AssertGet("/README.txt", readmeTxt)

	ctx.AssertGet("/config.json", configJSON)
}

func TestNestedDirectoryStructure(t *testing.T) {
	serverBlock := `@api_files {
		path /api/*.js
		file {path}
	}

	reverse_proxy @api_files {
		transport substrate
		to localhost
	}

	file_server`

	apiServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	const url = new URL(req.url);
	return new Response("API endpoint: " + url.pathname);
});`

	rootHTML := `<!DOCTYPE html><html><body><h1>Root Page</h1></body></html>`
	staticHTML := `<!DOCTYPE html><html><body><h1>Static Page</h1></body></html>`
	staticJS := `// Static JS in static directory
console.log("This is not a server");`

	files := []TestFile{
		{Path: "index.html", Content: rootHTML, Mode: 0644},
		{Path: "api/users.js", Content: apiServer, Mode: 0755},
		{Path: "api/orders.js", Content: apiServer, Mode: 0755},
		{Path: "static/page.html", Content: staticHTML, Mode: 0644},
		{Path: "static/script.js", Content: staticJS, Mode: 0644},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	ctx.AssertGet("/index.html", rootHTML)

	ctx.AssertGet("/api/users.js", "API endpoint: /api/users.js")
	ctx.AssertGet("/api/orders.js", "API endpoint: /api/orders.js")

	ctx.AssertGet("/static/page.html", staticHTML)
	ctx.AssertGet("/static/script.js", staticJS)
}
