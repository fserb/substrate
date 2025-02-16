# Substrate Caddy Module

## Overview

Substrate is a custom Caddy v2 module designed to execute commands based on HTTP requests.
It manages processes and proxies traffic to dynamically created backends.

What's special about it is that it allows the proxies themselves to tell Caddy which files
and paths they are set up to respond.

You can think of it as a generic Franken PHP for your own backend.

## Installation

To use this module, build Caddy with the Substrate module included:

```bash
xcaddy build --with github.com/fserb/substrate
```

## Configuration

Substrate provides a global app and a handler for HTTP requests.

### Global Configuration

To enable the module globally, use the `substrate` directive in the global options block:

```caddyfile
{
    substrate
}
```

### Handler Configuration

The `substrate` directive can be added to site blocks to define command execution logic. The syntax is:

```caddyfile
substrate {
    command <command> [arguments...]
    env <key> <value>
    user <username>
    dir <directory>
    restart_policy always|never|on_failure
    redirect_stdout stdout|stderr|null|file <filename>
    redirect_stderr stdout|stderr|null|file <filename>
}
```

### Directive Details

- **command**: Command to execute along with its arguments.
- **env**: Set environment variables for the process (can be repeated).
- **user**: Run the command as the specified user.
- **dir**: Set the working directory.
- **restart_policy**: Restart behavior.
  - `always`: Always restart when the process exits.
  - `never`: Never restart.
  - `on_failure`: Restart only if the process exits with a failure.
- **redirect_stdout**/**redirect_stderr**:
  - `stdout`: Output to stdout.
  - `stderr`: Output to stderr.
  - `null`: Discard output.
  - `file <filename>`: Write output to the specified file.

## Example Configuration

```caddyfile
example.com {
    substrate {
        command /usr/bin/myapp --flag value
        env FOO bar
        user appuser
        dir /var/myapp
        restart_policy on_failure
        redirect_stdout file /var/log/myapp/stdout.log
        redirect_stderr stderr
    }
}
```

## How It Works

Substrate will start your proxy command and set up a `SUBSTRATE` env var with a URL.
Once your proxy is running, it should send a `POST` request to this URL, containing a
JSON, with the following format:

```js
{
  host: "localhost:3333", // the host:port where the proxy is listening at.
  match: [".ext"],        // the list of file extensions we should listen to.
                          // this also automatically handles paths with the omitted
                          // extension or index.ext.
  paths: ["/up"],         // hard-coded paths that this proxy can answer.
  catch_all: ["/path/catchall.ext"],  // files that behave as catch-all for any subdir.
}
```

once this gets received, Caddy will start forwarding the proper requests to the proxy.
You can send multiple jsons to the provided URL, overwriting the previous commands.

For more details, check the example directory.

