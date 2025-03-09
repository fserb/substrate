# Substrate Caddy Module

## Overview

Substrate is a custom Caddy v2 module that enables dynamic backend execution based on HTTP requests. It automatically manages the lifecycle of backend processes and proxies traffic to them, creating a seamless integration between Caddy and your custom backend code.

The module watches for a special `substrate` executable file in your site's directory. When this file is present, Substrate launches it as a backend process and proxies relevant requests to it. When the file is removed or modified, Substrate automatically stops or restarts the process.

## Installation

To use this module, build Caddy with the Substrate module included:

```bash
xcaddy build --with github.com/fserb/substrate
```

## Configuration

Substrate provides both global configuration options and a handler for HTTP requests.

### Global Configuration

Configure global Substrate settings in the Caddyfile:

```caddyfile
{
    substrate {
        env KEY VALUE           # Set environment variables for substrate processes
        status_log stdout|stderr|null|file FILENAME  # Configure status logging
    }
}
```

- **env**: Set environment variables for all substrate processes (can be repeated)
- **status_log**: Configure where status logs are written
  - `stdout`: Output to standard output (default)
  - `stderr`: Output to standard error
  - `null`: Discard output
  - `file FILENAME`: Write output to the specified file

### Handler Configuration

Add the `substrate` directive to site blocks:

```caddyfile
example.com {
    root * /path/to/site
    substrate [/prefix]
    file_server
}
```

- The optional `/prefix` parameter specifies a URL path prefix for substrate requests
- If no prefix is specified, substrate will handle requests at the root path

## How It Works

1. Substrate watches for a file named `substrate` in your site's root directory
2. When the file is found, Substrate:
   - Executes it as a backend process
   - Passes a port number as the first argument
   - Waits for the process to start listening on that port
   - Proxies relevant requests to the backend

3. The backend process receives:
   - The port number as its first command-line argument
   - Standard environment variables plus any configured in the global settings
   - If running as root, the process runs with the permissions of the file owner

4. Request handling:
   - Requests matching the configured prefix are proxied to the backend
   - The backend can return status code 515 to indicate that Caddy should handle the request instead
   - Headers `X-Forwarded-Path` and `X-Forwarded-BaseURL` are added to proxied requests

5. Process management:
   - If the process exits, it's automatically restarted with exponential backoff
   - When the substrate file is removed, the process is stopped
   - When the substrate file is modified, the process is restarted

## Status Logging

Substrate provides detailed status logs to help with debugging:
- `[A]` messages: Administrative actions (starting/stopping processes)
- `[S]` messages: Standard output from the substrate process
- `[E]` messages: Standard error from the substrate process

## Examples

See the `example` directory for complete working examples.

