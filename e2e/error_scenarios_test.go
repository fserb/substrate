package e2e

import (
	"testing"
	"time"
)

func TestMissingFileReturnsError(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}

	file_server`

	files := []TestFile{}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/nonexistent.js", 404)
}

func TestSlowStartupHandling(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			startup_timeout 1s
		}
		to localhost
	}`

	slowStartupServer := `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
console.log("Starting slow server...");

// Simulate slow startup
await new Promise(resolve => setTimeout(resolve, 300));

console.log("Slow server ready");
Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("Slow server finally responded!");
});`

	files := []TestFile{
		{Path: "slow.js", Content: slowStartupServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	start := time.Now()
	ctx.AssertGet("/slow.js", "Slow server finally responded!")
	duration := time.Since(start)

	if duration < 300*time.Millisecond {
		t.Errorf("Request completed too quickly: %v", duration)
	}

	t.Logf("Slow startup request took: %v", duration)
}

func TestVerySlowStartupTimeout(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			startup_timeout 200ms
		}
		to localhost
	}`

	verySlowServer := `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
console.log("Starting very slow server...");

// Simulate very slow startup (longer than timeout)
await new Promise(resolve => setTimeout(resolve, 500));

console.log("Very slow server ready");
Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("This should timeout");
});`

	files := []TestFile{
		{Path: "very_slow.js", Content: verySlowServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/very_slow.js", 502)
}

func TestServerThatFailsToStart(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	failingServer := `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
// This will cause a syntax error
this is not valid javascript code!!!
Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response("This won't work");
});`

	files := []TestFile{
		{Path: "failing.js", Content: failingServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/failing.js", 502)
}

func TestServerThatBindsToWrongPort(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	wrongPortServer := `#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
console.log("Args:", Deno.args);
// Ignore the provided socket and use a different one
Deno.serve({path: "/tmp/wrong-socket.sock"}, (req) => {
	return new Response("Wrong socket server");
});`

	files := []TestFile{
		{Path: "wrong_port.js", Content: wrongPortServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	ctx.AssertGetStatus("/wrong_port.js", 502)
}
