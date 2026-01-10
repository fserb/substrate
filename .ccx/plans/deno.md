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
status: done
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
status: done
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
status: done
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
status: done
depends: 4
priority: 0
files: *_test.go

Run `./task test` and ensure all unit/integration tests pass with the new Deno execution model.

---

### 6. Add common server block helper to testutil.go
status: done
depends: 5
priority: 1
files: e2e/testutil.go

Most tests use identical server block patterns. Add `StandardServerBlock()` and `ServerBlockWithConfig(opts)` helpers to reduce duplication.

```go
// Current: Every test repeats this
serverBlock := `@js_files {
    path *.js
    file {path}
}
reverse_proxy @js_files {
    transport substrate
    to localhost
}`

// Target: Single helper call
serverBlock := StandardServerBlock()
// or with config:
serverBlock := ServerBlockWithConfig(SubstrateConfig{IdleTimeout: "-1"})
```

---

### 7. Merge one-shot idle timeout tests
status: done
depends: 5
priority: 1
files: e2e/idle_timeout_oneshot_test.go, e2e/idle_timeout_oneshot_cleanup_test.go

Merge `idle_timeout_oneshot_test.go` and `idle_timeout_oneshot_cleanup_test.go` into a single `idle_timeout_test.go`. Both test the same feature (idle_timeout -1 mode).

---

### 8. Merge process lifecycle tests
status: done
depends: 5
priority: 1
files: e2e/process_reuse_test.go, e2e/process_restart_test.go

Merge `process_reuse_test.go` and `process_restart_test.go` into `process_lifecycle_test.go`. Both test related process behavior (reuse vs restart after exit/crash).

---

### 9. Merge error scenario tests
status: done
depends: 5
priority: 1
files: e2e/error_scenarios_test.go, e2e/internal_ip_error_test.go

Merge `internal_ip_error_test.go` into `error_scenarios_test.go`. Both cover error handling - the internal IP test just verifies detailed error messages are returned.

---

### 10. Consolidate concurrent tests
status: done
depends: 5
priority: 2
files: e2e/concurrent_requests_test.go

Simplify the three tests in `concurrent_requests_test.go`. `TestConcurrentRequestsToSameProcess` and `TestHighConcurrencyToSingleProcess` do essentially the same thing with different request counts.

---

### 11. Simplify non_executable tests
status: done
depends: 5
priority: 2
files: e2e/non_executable_test.go

`TestNonExecutableFilesWork` and `TestReadOnlyFileWorks` are redundant - both just verify that file permissions don't matter when running via Deno. Combine into a single test.

---

### 12. Merge process output tests
status: done
depends: 5
priority: 2
files: e2e/process_output_test.go

`TestProcessStdoutLogging` and `TestProcessStderrLogging` are nearly identical. Combine into a single test that verifies both stdout and stderr logging work.

---

### 13. Add AssertGetBody helper to testutil.go
status: done
depends: 5
priority: 2
files: e2e/testutil.go

Many tests need to read response bodies for complex assertions. Add helper:

```go
func (ctx *E2ETestContext) GetBody(path string) (string, int) {
    // Returns body and status code
}
```

This simplifies tests that need to do JSON parsing or string matching on response bodies.

---

### 14. Review privilege dropping for Deno execution
status: done
depends: 5
priority: 1
files: process_security.go

`configureProcessSecurity()` drops privileges to the script file's owner/group when running as root. Review if this still makes sense with Deno:

- Currently: If root runs substrate and script is owned by `user`, deno runs as `user`
- Question: Should we drop to script owner, or use a dedicated substrate user?
- The current approach may still be reasonable (script owner controls execution)
- Consider documenting the security model explicitly

---

### 15. Rename Process.Command to Process.ScriptPath
status: done
depends: 5
priority: 2
files: process.go

The `Command` field was the path to an executable. Now it's the path to a JS script that Deno runs. Rename for clarity:

```go
// Current
type Process struct {
    Command    string
    // ...
}

// Target
type Process struct {
    ScriptPath string
    // ...
}
```

---

### 16. Remove Mode: 0755 from e2e test files
status: done
depends: 5
priority: 2
files: e2e/*_test.go

All e2e tests set `Mode: 0755` on JS files, but this is now unnecessary since Deno runs them regardless of executable permission. Remove or use default (0644).

---

### 17. Fix process_output_test.go .sh extension
status: done
depends: 5
priority: 2
files: e2e/process_output_test.go

`TestProcessOutputWithCrash` uses `.sh` extension for JavaScript content:
```go
{Path: "crash_test.sh", Content: `const [socketPath] = Deno.args; ...`}
```

Should be `.js` to match the content.

---

### 18. Update CLAUDE.md documentation
status: pending
depends: 5
priority: 2
files: CLAUDE.md

Update documentation to reflect Deno execution model:
- Remove references to shebangs in example code
- Remove mention of executable permission requirements
- Update "Process Protocol" section - files are now JS scripts, not executables
- Clarify that substrate manages its own Deno runtime

---

### 19. Clean up old comments referencing direct execution
status: pending
depends: 5
priority: 3
files: process.go, substrate.go

Search for comments that reference the old execution model and update or remove:
- References to "executable" files
- References to shebang lines
- References to running files "directly"

---

### 20. Simplify process_security_test.go
status: pending
depends: 5
priority: 2
files: process_security_test.go

Tests still create shell scripts with shebangs when testing configureProcessSecurity. These can be simplified to plain text files since we no longer check executable permission:

```go
// Current
scriptContent := "#!/bin/bash\necho 'hello world'\n"

// Target - just test privilege dropping, not execution
fileContent := "test content"
```

---

### 21. Consider removing symlink-specific tests
status: pending
depends: 5
priority: 3
files: e2e/working_directory_test.go, process_security_test.go

Symlink handling was important when checking executable permissions (symlink target vs symlink itself). With Deno, symlinks are transparent - Deno just reads whatever the path resolves to. Review if symlink-specific tests are still needed or can be simplified.

## Notes

- 2026-01-10: Plan created
- 2026-01-10: Aligned deno.go with pop's tested implementation: file-based zip extraction, simpler validation, proper cache path structure
- 2026-01-10: Added StandardServerBlock() and ServerBlockWithConfig(SubstrateConfig) helpers to e2e/testutil.go. Updated simple_test.go, process_reuse_test.go, concurrent_requests_test.go, idle_timeout_oneshot_test.go, and idle_timeout_oneshot_cleanup_test.go to use the new helpers instead of duplicating server block patterns.
- 2026-01-10: Merged idle_timeout_oneshot_test.go and idle_timeout_oneshot_cleanup_test.go into idle_timeout_test.go with two tests: TestOneShotModeStateReset (counter reset) and TestOneShotModeProcessCleanup (PID verification)
- 2026-01-10: Merged process_reuse_test.go and process_restart_test.go into process_lifecycle_test.go. The new file contains 4 tests: TestProcessReusesForMultipleRequests, TestDifferentFilesGetDifferentProcesses, TestProcessRestartsAfterExit, and TestProcessRestartAfterCrash. Updated restart tests to use StandardServerBlock() helper.
- 2026-01-10: Merged internal_ip_error_test.go into error_scenarios_test.go. Added TestDetailedErrorForInternalIP and TestProcessStartupTimeoutWithDetailedError tests. Deleted the old file.
- 2026-01-10: Reviewed privilege dropping for Deno execution. Confirmed current approach (script owner controls execution) is reasonable. Added explicit Security Model section to CLAUDE.md documenting: privilege dropping behavior, security properties, configuration considerations, and rationale for script-owner approach vs dedicated user. Also updated process_security.go comment with detailed security model documentation.
- 2026-01-10: Consolidated concurrent tests - merged TestConcurrentRequestsToSameProcess and TestHighConcurrencyToSingleProcess into a single test with 8 concurrent requests. Removed Mode: 0755 from test files since it's no longer needed with Deno execution.
- 2026-01-10: Combined TestNonExecutableFilesWork and TestReadOnlyFileWorks into single TestFilePermissionsDontMatter test that verifies 0755, 0644, and 0444 file permissions all work via Deno. Updated to use StandardServerBlock() helper.
- 2026-01-10: Merged TestProcessStdoutLogging and TestProcessStderrLogging into single TestProcessOutputLogging test. Updated to use ServerBlockWithConfig() helper and removed Mode: 0755. The combined test exercises both stdout and stderr logging by using console.log and console.error in the test script.
- 2026-01-10: Added GetBody(path) helper to E2ETestContext that returns body string and status code. Updated idle_timeout_test.go to use the new helper, removing direct io.ReadAll usage.
- 2026-01-10: Renamed Process.Command to Process.ScriptPath for clarity since it now represents a JS script path rather than an executable. Also renamed ProcessStartupError.Command to ScriptPath and updated all log messages from 'command' to 'script_path'. Updated variable names in ProcessManager functions (removeProcess, cleanupIdleProcesses, Stop) to use scriptPath instead of command.
- 2026-01-10: Removed Mode: 0755 from all e2e test files (36 occurrences across 15 test files). Files now use the default mode (0644) since Deno doesn't require executable permission. Simplified non_executable_test.go by consolidating three tests into two (removed redundant 0755 and 0644 tests, kept only default and 0444 readonly). All tests pass.