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

	// Dynamic server
	dynamicServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Dynamic content from: " + req.url);
});`

	// Static HTML file
	indexHTML := `<!DOCTYPE html>
<html>
<head><title>Mixed Content Test</title></head>
<body>
	<h1>Static HTML Page</h1>
	<p>This page contains both static and dynamic content.</p>
</body>
</html>`

	// Static CSS file
	styleCSS := `body {
	font-family: Arial, sans-serif;
	margin: 20px;
	background-color: #f0f0f0;
}

h1 {
	color: #333;
	border-bottom: 2px solid #007acc;
}`

	// Static JavaScript file (not executable)
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
		{Path: "client.js", Content: clientJS, Mode: 0644}, // Not executable
		{Path: "api/server.js", Content: dynamicServer, Mode: 0755}, // Executable - becomes server
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// Test static HTML file
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/index.html", 200, indexHTML)

	// Test static CSS file
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/style.css", 200, styleCSS)

	// Test static JavaScript file (served as static, not executed)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/client.js", 200, clientJS)

	// Test dynamic JavaScript server
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/api/server.js", 200, "Dynamic content from: "+ctx.BaseURL+"/api/server.js")
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

	// Python server (simulated with Deno for consistency)
	pythonServer := `#!/usr/bin/env -S deno run --allow-net
// Simulating Python server
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Python-like server response from: " + req.url);
});`

	// Shell script server (simulated with Deno for consistency)
	shellServer := `#!/usr/bin/env -S deno run --allow-net
// Simulating shell script server
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Shell-like server response from: " + req.url);
});`

	// Static text file
	readmeTxt := `# Project README

This is a static text file that should be served directly.

It contains information about the project.`

	// Static JSON file
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

	// Test Python server (proxied)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/server.py", 200, "Python-like server response from: "+ctx.BaseURL+"/server.py")

	// Test Shell script server (proxied)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/script.sh", 200, "Shell-like server response from: "+ctx.BaseURL+"/script.sh")

	// Test static text file
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/README.txt", 200, readmeTxt)

	// Test static JSON file
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/config.json", 200, configJSON)
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

	// API server in subdirectory
	apiServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	const url = new URL(req.url);
	return new Response("API endpoint: " + url.pathname);
});`

	// Static files in various directories
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

	// Test root static file
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/index.html", 200, rootHTML)

	// Test API endpoints (proxied)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/api/users.js", 200, "API endpoint: /api/users.js")
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/api/orders.js", 200, "API endpoint: /api/orders.js")

	// Test static files in subdirectory
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/static/page.html", 200, staticHTML)
	ctx.Tester.AssertGetResponse(ctx.BaseURL+"/static/script.js", 200, staticJS)
}