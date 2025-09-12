package e2e

import (
	"testing"
)

func TestNonExecutableFilesReturnError(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	executableServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("Executable server response");
});`

	nonExecutableServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("This should not run");
});`

	files := []TestFile{
		{Path: "executable.js", Content: executableServer, Mode: 0755},
		{Path: "non_executable.js", Content: nonExecutableServer, Mode: 0644},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGet("/executable.js", "Executable server response")

	ctx.AssertGetStatus("/non_executable.js", 502)
}

func TestReadOnlyFileReturnError(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	readOnlyServer := `#!/usr/bin/env -S deno run --allow-net
Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, (req) => {
	return new Response("This should not run");
});`

	files := []TestFile{
		{Path: "readonly.js", Content: readOnlyServer, Mode: 0444},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/readonly.js", 502)
}
