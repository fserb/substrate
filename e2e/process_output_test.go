package e2e

import (
	"testing"
	"time"
)

func TestProcessStdoutLogging(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate {
			idle_timeout 1m
			startup_timeout 10s
		}
		to localhost
	}`

	files := []TestFile{
		{
			Path: "stdout_test.js",
			Content: `#!/usr/bin/env -S deno run --allow-net
const [host, port] = Deno.args;

// Log some messages to stdout and stderr
console.log("Starting server on " + host + ":" + port);
console.error("This is an error message");
console.log("Server ready");

const server = Deno.serve({
  hostname: host === "localhost" ? "127.0.0.1" : host,
  port: parseInt(port)
}, (req) => {
  console.log("Handling request to: " + req.url);
  return new Response("Hello from stdout test!");
});

// Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  console.log("Shutting down server");
  server.shutdown();
  Deno.exit(0);
});
`,
			Mode: 0755,
		},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make a request to trigger process startup and logging
	ctx.AssertGet("/stdout_test.js", "Hello from stdout test!")

	// Give some time for logs to be processed
	time.Sleep(100 * time.Millisecond)

	// Make another request to trigger more logging
	ctx.AssertGet("/stdout_test.js", "Hello from stdout test!")
}

func TestProcessStderrLogging(t *testing.T) {
	serverBlock := `@py_files {
		path *.py
		file {path}
	}

	reverse_proxy @py_files {
		transport substrate {
			idle_timeout 1m
			startup_timeout 10s
		}
		to localhost
	}`

	files := []TestFile{
		{
			Path: "stderr_test.py",
			Content: `#!/usr/bin/env python3
import sys
import http.server
import socketserver
import threading

def main():
    if len(sys.argv) < 3:
        print("Usage: script.py <host> <port>", file=sys.stderr)
        sys.exit(1)

    host = sys.argv[1]
    port = int(sys.argv[2])

    # Log messages to both stdout and stderr
    print(f"Starting Python server on {host}:{port}")
    print(f"Warning: This is a test warning", file=sys.stderr)
    print("Server initialization complete")

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            print(f"Processing request: {self.path}")
            print(f"Debug: Request from {self.client_address}", file=sys.stderr)
            self.send_response(200)
            self.send_header('Content-type', 'text/plain')
            self.end_headers()
            self.wfile.write(b"Hello from stderr test!")

        def log_message(self, format, *args):
            # Suppress default HTTP server logs
            pass

    with socketserver.TCPServer((host if host != "localhost" else "127.0.0.1", port), Handler) as httpd:
        print(f"Server ready on {host}:{port}")

        def shutdown_handler():
            import signal
            def signal_handler(signum, frame):
                print("Received shutdown signal", file=sys.stderr)
                print("Shutting down server")
                httpd.shutdown()
            signal.signal(signal.SIGTERM, signal_handler)

        shutdown_thread = threading.Thread(target=shutdown_handler)
        shutdown_thread.daemon = True
        shutdown_thread.start()

        httpd.serve_forever()

if __name__ == "__main__":
    main()
`,
			Mode: 0755,
		},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make a request to trigger process startup and logging
	ctx.AssertGet("/stderr_test.py", "Hello from stderr test!")

	// Give some time for logs to be processed
	time.Sleep(100 * time.Millisecond)

	// Make another request to trigger more logging
	ctx.AssertGet("/stderr_test.py", "Hello from stderr test!")
}

func TestProcessOutputWithCrash(t *testing.T) {
	serverBlock := `@crash_files {
		path *.sh
		file {path}
	}

	reverse_proxy @crash_files {
		transport substrate {
			idle_timeout 1m
			startup_timeout 5s
		}
		to localhost
	}`

	files := []TestFile{
		{
			Path: "crash_test.sh",
			Content: `#!/bin/bash
host=$1
port=$2

echo "Starting crash test server on $host:$port"
echo "This server will crash after starting" >&2

# Start a simple HTTP server that crashes
python3 -c "
import http.server
import socketserver
import sys
import time

host = '$host' if '$host' != 'localhost' else '127.0.0.1'
port = int('$port')

print('Python server starting on ' + host + ':' + str(port))
print('Warning: Server will crash soon', file=sys.stderr)

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        print('Handling request before crash')
        print('About to crash!', file=sys.stderr)
        self.send_response(200)
        self.send_header('Content-type', 'text/plain')
        self.end_headers()
        self.wfile.write(b'Response before crash')
        # Crash after first request
        sys.exit(1)

    def log_message(self, format, *args):
        pass

with socketserver.TCPServer((host, port), Handler) as httpd:
    print('Server ready, waiting for crash trigger')
    httpd.serve_forever()
"
`,
			Mode: 0755,
		},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	// Make a request that should cause the process to crash
	ctx.AssertGet("/crash_test.sh", "Response before crash")

	// Give time for crash logging
	time.Sleep(200 * time.Millisecond)

	// Second request should create a new process
	ctx.AssertGet("/crash_test.sh", "Response before crash")
}