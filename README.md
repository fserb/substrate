# Substrate

A Caddy module that adds a custom transport method for `reverse_proxy`, enabling dynamic process execution based on file requests.

## Overview

Substrate behaves like FastCGI but over HTTP - it executes requested files as separate processes and proxies HTTP traffic to them. Each file gets its own process with automatic lifecycle management.

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
#!/usr/bin/env -S deno run --allow-net
const [host, port] = Deno.args;

Deno.serve({ 
  hostname: host, 
  port: parseInt(port) 
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
2. **Process Creation**: Substrate executes the file with `host` and `port` arguments
3. **Port Management**: Each file gets a unique port automatically assigned
4. **Request Proxying**: HTTP requests are proxied to the running process
5. **Lifecycle Management**: Processes are reused, restarted, and cleaned up automatically

## Process Contract

Your executable receives two arguments:
- `argv[1]`: Host to bind to (usually "localhost")
- `argv[2]`: Port to listen on (unique per file)

Example in various languages:

**Deno/Node.js:**
```javascript
#!/usr/bin/env -S deno run --allow-net
const [host, port] = Deno.args;
// Start HTTP server on host:port
```

**Python:**
```python
#!/usr/bin/env python3
import sys
host, port = sys.argv[1], int(sys.argv[2])
# Start HTTP server on host:port
```

**Go:**
```go
//go:build ignore

package main
import ("fmt"; "net/http"; "os")

func main() {
    host, port := os.Args[1], os.Args[2]
    http.ListenAndServe(host+":"+port, handler)
}
```

## Configuration

### Transport Options

```
reverse_proxy @matcher {
    transport substrate {
        idle_timeout 5m      # How long to keep unused processes
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

- **Zero Configuration**: Processes just need to listen on the provided port
- **Automatic Port Management**: No port conflicts or manual assignment
- **Process Reuse**: Same file requests share the same process
- **Hot Reloading**: File changes restart the associated process
- **Concurrent Safe**: Multiple requests handled properly
- **Resource Cleanup**: Idle processes automatically terminated

## Development

```bash
./task build    # Build the module
./task test     # Run unit tests
./task e2e      # Run end-to-end tests (requires Deno)
./task run      # Run example configuration
```

## Examples

See the test files in `testdata/` for working examples in Deno, or check the integration tests for comprehensive usage patterns.