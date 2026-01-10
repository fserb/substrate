# Substrate

A Caddy module that adds a custom transport method for `reverse_proxy`, enabling dynamic JavaScript execution via Deno.

## Overview

Substrate behaves like FastCGI but over HTTP - it runs JavaScript files as separate Deno processes and proxies HTTP traffic to them via Unix domain sockets. Each file gets its own process with automatic lifecycle management.

Substrate automatically downloads and manages its own Deno runtime, so no external dependencies are required.

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

2. Create a JavaScript file (e.g., `hello.js`):
```javascript
const [socketPath] = Deno.args;

Deno.serve({
  path: socketPath
}, (req) => {
  return new Response('Hello from Substrate!');
});
```

3. Start Caddy:
```bash
caddy run
```

4. Request triggers process execution:
```bash
curl http://localhost/hello.js
# → "Hello from Substrate!"
```

## How It Works

1. **File Matching**: Caddy's file matcher identifies JavaScript files
2. **Deno Runtime**: Substrate downloads and caches Deno automatically on first use
3. **Process Creation**: Substrate runs the file via `deno run --allow-all script.js socketPath`
4. **Socket Management**: Each file gets a unique Unix domain socket automatically assigned
5. **Request Proxying**: HTTP requests are proxied to the running process via Unix socket
6. **Lifecycle Management**: Processes are reused, restarted, and cleaned up automatically

## Process Contract

Your JavaScript file receives one argument:
- `Deno.args[0]`: Unix socket path to listen on (e.g., `/tmp/substrate-abc123.sock`)

Scripts do not need shebang lines or executable permission - Substrate handles execution via its embedded Deno runtime.

**Example:**
```javascript
const [socketPath] = Deno.args;

Deno.serve({ path: socketPath }, (req) => {
  return new Response('Hello!');
});

// Optional: Graceful shutdown
Deno.addSignalListener("SIGTERM", () => {
  Deno.exit(0);
});
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

### Idle Timeout Modes

- **Positive values** (e.g., `5m`): Normal operation - cleanup after idle period
- **Zero** (`0`): Processes run indefinitely until manually stopped
- **Negative one** (`-1`): One-shot mode - process terminates after each request

## Features

- **Zero Configuration**: Scripts just need to listen on the provided Unix socket
- **Automatic Deno Management**: Deno runtime downloaded and cached automatically
- **Automatic Socket Management**: Each process gets a unique Unix domain socket
- **Process Reuse**: Same file requests share the same process
- **Hot Reloading**: File changes restart the associated process
- **Concurrent Safe**: Multiple requests handled properly
- **Resource Cleanup**: Idle processes and socket files automatically cleaned up
- **Security**: Unix socket isolation and privilege dropping when running as root
- **Advanced Routing**: URL rewriting, subpath matching, and pattern-based routing

## Development

```bash
./task build    # Build the module
./task test     # Run all tests (unit + integration + e2e)
./task run      # Run example configuration
```

## Advanced Usage

### URL Rewriting
Route clean URLs to JavaScript scripts:
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
