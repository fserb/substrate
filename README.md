# Substrate

A Caddy module that adds a custom transport method for `reverse_proxy`, enabling dynamic process execution based on file requests.

## Overview

Substrate behaves like FastCGI but over HTTP - it executes requested files as separate processes and proxies HTTP traffic to them via Unix domain sockets. Each file gets its own process with automatic lifecycle management.

## Installation

Build Caddy with the Substrate module:

```bash
xcaddy build --with github.com/fserb/substrate
```

## Quick Start

1. Create a Caddyfile:
```
root /path/to/your/files

@js_files {
    path *.js
    file {path}
}

reverse_proxy @js_files {
    transport substrate {
        idle_timeout 5m
        startup_timeout 30s
    }
}
```

2. Create an executable script (e.g., `hello.js`):
```javascript
#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
const [socketPath] = Deno.args;

Deno.serve({
  path: socketPath
}, (req) => {
  return new Response('Hello from Substrate!');
});
```

3. Make it executable and start Caddy:
```bash
chmod +x hello.js
caddy run
```

4. Request triggers process execution:
```bash
curl http://localhost/hello.js
# → "Hello from Substrate!"
```

## How It Works

1. **File Matching**: Caddy's file matcher identifies executable files
2. **Process Creation**: Substrate executes the file with a Unix socket path argument
3. **Socket Management**: Each file gets a unique Unix domain socket automatically assigned
4. **Request Proxying**: HTTP requests are proxied to the running process via Unix socket
5. **Lifecycle Management**: Processes are reused, restarted, and cleaned up automatically

## Process Contract

Your executable receives one argument:
- `argv[1]`: Unix socket path to listen on (e.g., `/tmp/substrate-abc123.sock`)

Example in various languages:

**Deno:**
```javascript
#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
const [socketPath] = Deno.args;

Deno.serve({ path: socketPath }, (req) => {
  return new Response('Hello!');
});
```

**Python:**
```python
#!/usr/bin/env python3
import sys
import socket
import http.server
import socketserver

socket_path = sys.argv[1]

class UnixHTTPServer(socketserver.UnixStreamServer):
    def server_bind(self):
        socketserver.UnixStreamServer.server_bind(self)

with UnixHTTPServer(socket_path, http.server.SimpleHTTPRequestHandler) as httpd:
    httpd.serve_forever()
```

**Go:**
```go
//go:build ignore

package main
import ("net"; "net/http"; "os")

func main() {
    socketPath := os.Args[1]
    listener, _ := net.Listen("unix", socketPath)
    http.Serve(listener, handler)
}
```

## Configuration

### Transport Options

```
reverse_proxy @matcher {
    transport substrate {
        idle_timeout 5m      # How long to keep unused processes (0=never cleanup, -1=close after request)
        startup_timeout 30s  # How long to wait for process startup
    }
}
```

### Multiple File Types

```
@scripts {
    path *.js *.py *.go
    file {path}
}

reverse_proxy @scripts {
    transport substrate
}
```

## Features

- **Zero Configuration**: Processes just need to listen on the provided Unix socket
- **Automatic Socket Management**: Each process gets a unique Unix domain socket
- **Process Reuse**: Same file requests share the same process
- **Hot Reloading**: File changes restart the associated process
- **Concurrent Safe**: Multiple requests handled properly
- **Resource Cleanup**: Idle processes and socket files automatically cleaned up
- **Security**: Executable validation, Unix socket isolation, and privilege dropping when running as root
- **Advanced Routing**: URL rewriting, subpath matching, and pattern-based routing

## Development

```bash
./task build    # Build the module
./task test     # Run all tests (unit + integration + e2e)
./task run      # Run example configuration
```

## Advanced Usage

### URL Rewriting
Route clean URLs to executable scripts:
```
@simple_rewrite {
    not path *.js
    file {path}.js
}

reverse_proxy @simple_rewrite {
    transport substrate
}
```

### Subpath Routing
Extract subpaths and forward as headers:
```
@subpath_match {
    path_regexp m ^(.*)(/[^/]+)$
}

handle @subpath_match {
    @file_exists file {re.m.1}.lemon.js
    handle @file_exists {
        reverse_proxy {
            header_up X-Subpath {re.m.2}
            transport substrate
        }
    }
}
```

## Examples

Check the e2e tests in `e2e/` directory for comprehensive usage patterns and working examples.