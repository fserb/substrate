# Plan: Deno Native Support for Substrate

status: active
created: 2026-01-10

## Prompt

Replace the generic executable transport with Deno-native execution. Instead of running scripts directly as executables, substrate should run them via `deno run` with full permissions. Substrate should download and cache its own Deno binary (like pop does) rather than requiring system Deno.

## Goal

- Scripts are executed via `deno run --allow-all script.js socketPath`
- Deno binary is automatically downloaded and cached (~/.cache/substrate/deno/)
- Scripts no longer need shebang or executable permission
- HTTP-over-Unix-socket model unchanged (scripts still create HTTP servers)
- All existing e2e tests pass (updated to use .js files without shebangs)

## Context

- Current execution: `exec.Command(filePath, socketPath)` - runs file as executable
- Pop reference: `~/ts/pop/shared/deno.go` - downloads Deno from GitHub releases
- Pop reference: `~/ts/pop/shared/run.go` - shows deno invocation pattern
- Key files: `process.go` (process management), `process_security.go` (validation)

## Tasks

### 1. DenoManager downloads and caches Deno binary
status: done
depends: none
priority: 0
files: deno.go (new)

Add DenoManager that downloads Deno from GitHub releases and caches it. Adapted from pop's implementation.

```go
// Current: no Deno management
// Target:
dm := NewDenoManager(logger)
denoPath, err := dm.Get()  // Downloads if needed, returns path
```

DenoManager should:
- Cache in `~/.cache/substrate/deno/{version}-{platform}/deno`
- Download from `https://github.com/denoland/deno/releases/download/...`
- Support darwin (arm64/amd64) and linux (amd64)
- Validate binary runs `deno --version` successfully

---

### 2. Process starts scripts via deno run
status: pending
depends: 1
priority: 0
files: process.go

Change process startup to use `deno run` instead of direct execution.

```go
// Current (process.go:408):
p.Cmd = exec.Command(p.Command, args...)

// Target:
denoPath, _ := dm.Get()
p.Cmd = exec.Command(denoPath, "run", "--allow-all", p.Command, p.SocketPath)
```

ProcessManager needs access to DenoManager (created during Provision).

---

### 3. File validation accepts .js files without executable check
status: pending
depends: 2
priority: 0
files: process.go, process_security.go

Remove executable permission check since Deno runs the file, not the OS.

```go
// Current (process_security.go:17):
if err := unix.Access(filePath, unix.X_OK); err != nil {
    return fmt.Errorf("file %s is not executable: %w", filePath, err)
}

// Target: Remove this check entirely
```

Keep the privilege-dropping logic (run deno as file owner when root).

---

### 4. E2E tests updated for Deno execution
status: pending
depends: 3
priority: 0
files: e2e/*_test.go, test scripts

Update test scripts to be plain .js files (no shebang, no executable bit).
Tests should still pass since deno runs them.

```javascript
// Current test script:
#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write
const [socketPath] = Deno.args;
Deno.serve({ path: socketPath }, ...);

// Target test script:
const [socketPath] = Deno.args;
Deno.serve({ path: socketPath }, ...);
```

---

### 5. Integration tests pass
status: pending
depends: 4
priority: 0
files: *_test.go

Run `./task test` and ensure all unit/integration tests pass with the new Deno execution model.

## Notes

- 2026-01-10: Plan created