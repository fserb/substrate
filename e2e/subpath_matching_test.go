package e2e

import (
	"testing"
)

func TestSubpathMatching(t *testing.T) {
	// Caddyfile block that matches "x/y" and rewrites to "x.lemon.js"
	serverBlock := `@subpath_match {
		path_regexp m ^(.*)(/[^/]+)$
	}

	handle @subpath_match {
		@file_exists file {re.m.1}.lemon.js
		handle @file_exists {
			reverse_proxy {
				header_up X-Subpath {re.m.2}
				transport substrate
				to localhost
			}
		}
	}`

	subpathEchoServer := `Deno.serve({path: Deno.args[0]}, (req) => {
	const originalPath = new URL(req.url).pathname;
	const subpath = req.headers.get('X-Subpath') || '';
	return new Response(` + "`path:${originalPath} subpath:${subpath}`" + `);
});`

	files := []TestFile{
		{Path: "app.lemon.js", Content: subpathEchoServer, Mode: 0755},
		{Path: "api.lemon.js", Content: subpathEchoServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGet("/app/users", "path:/app/users subpath:/users")
	ctx.AssertGet("/api/v1", "path:/api/v1 subpath:/v1")
	ctx.AssertGet("/app/settings", "path:/app/settings subpath:/settings")
}
