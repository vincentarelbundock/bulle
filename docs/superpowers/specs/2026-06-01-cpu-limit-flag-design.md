# CPU Limit Flag Design

## Summary

Add a `--cpu-limit DURATION` CLI flag that caps CPU time for sandboxed commands. The duration uses Go syntax accepted by `time.ParseDuration`, such as `30s`, `2m`, or `1h30m`, but must resolve to whole seconds because portable Unix CPU limits are second-granularity. Omitted CPU limit and `--cpu-limit 0` preserve current behavior and do not apply a CPU resource limit.

The implementation uses `RLIMIT_CPU`, which is available on Linux and macOS and does not require external tools or elevated privileges. This is a per-process CPU-time limit inherited by the sandboxed command and its descendants, not an aggregate CPU budget for the whole process tree.

## Goals

- Provide a portable CPU-time limit for sandboxed commands on Linux and macOS.
- Accept explicit Go-style duration values with units.
- Reject ambiguous or unenforceable CPU limit values before sandbox setup begins.
- Apply the CPU limit only to the prepared runner and sandboxed command, not to the public `bulle` parent process.
- Preserve normal behavior when no CPU limit is configured.
- Preserve existing exit codes for successful commands, failed commands, missing executables, and non-executable paths.
- Keep CPU limit handling inside `bulle`; do not depend on shell wrappers, `ulimit`, cgroups, launchd jobs, or external tools.
- Keep CPU limit handling compatible with the timeout supervisor and interactive commands.

## Non-Goals

- No aggregate CPU accounting across the whole command tree in this change.
- No CPU throttling, CPU share, core-count, or scheduling-priority controls.
- No memory, disk, process-count, or file-descriptor quotas in this change.
- No idle timeout or wall-clock timeout changes beyond sharing the supervisor path when needed.
- No per-profile CPU limit defaults in config files.
- No user-configurable `SIGXCPU` grace period.
- No changes to sandbox permission semantics.

## User Interface

Add a top-level flag:

```text
--cpu-limit DURATION
                  limit per-process CPU time for the sandboxed command
```

Examples:

```text
bulle --cpu-limit 30s --add-exec -- /usr/bin/yes
bulle --cpu-limit 2m ~/repos/project --profile codex -- codex
bulle --timeout 10m --cpu-limit 5m . -- claude
```

Accepted values:

- `0`: no CPU limit.
- Any positive duration accepted by `time.ParseDuration` that is at least `1s` and resolves to whole seconds, including combined values such as `1h30m`.

Rejected values:

- Negative durations, such as `-1s`.
- Unitless nonzero values, such as `30`.
- Sub-second or fractional-second values, such as `500ms` or `1.5s`.
- Malformed duration strings, such as `ten seconds`.

The parser error should name the invalid value and state that `--cpu-limit` expects a Go duration with whole-second granularity, such as `30s`, `2m`, or `1h30m`.

`--cpu-limit` does not apply in immediate non-command modes because they do not start a sandboxed command:

- `help`
- `version`
- `--list-profiles`
- `--install-profiles`
- `--policy`

Policy output should still include the CPU limit value when one is configured so callers can inspect the resolved invocation before running it.

## CPU Limit Semantics

`--cpu-limit` limits CPU time, not wall-clock time. A command that mostly sleeps or waits for I/O can run much longer than the configured value without exceeding the CPU limit. Use `--timeout` for wall-clock deadlines.

The limit is per process. Each process in the sandboxed command tree inherits the configured `RLIMIT_CPU` value, but CPU usage is not summed across siblings. A command that repeatedly spawns fresh worker processes can consume more total CPU than the configured value across the whole tree. That stronger aggregate behavior is a separate future feature because it requires platform-specific process accounting or cgroup-style support that is not portable to both Linux and macOS.

The implementation should normally set the soft CPU limit to the configured whole-second value and the hard CPU limit to one second above it. The hard-limit second is an implementation grace window for processes that catch or ignore `SIGXCPU`; it is not user-configurable. If the current process already has a finite hard CPU limit, the implementation must not try to raise it.

## Exit Behavior

When the command completes without exceeding the CPU limit, `bulle` preserves current exit behavior for the active execution path:

- `0` when the sandboxed command succeeds.
- Nonzero command exits keep the same observable behavior as they have without this feature.
- `126` when the command exists but is not executable.
- `127` when the command cannot be found.
- Existing setup and validation exit codes remain unchanged.

When the supervised runner exits because the CPU limit is exceeded, `bulle` returns exit code `125`.

On CPU limit exhaustion, stderr prints one line:

```text
bulle: command exceeded CPU limit of 30s
```

The duration in the message should be the normalized Go duration string for the parsed value.

When both `--timeout` and `--cpu-limit` are configured, whichever limit is observed first determines the exit status:

- Wall-clock timeout first: exit `124` and print the timeout message.
- CPU limit first: exit `125` and print the CPU-limit message.
- Normal command exit first: preserve normal command exit behavior.

## Architecture

CPU enforcement needs to happen in the child execution path so the public `bulle` process is not permanently constrained by `setrlimit`. The existing timeout design already creates a parent supervisor and hidden prepared-policy runner. Reuse that structure for CPU limits.

Use a shared supervised path whenever either `--timeout` or `--cpu-limit` is configured:

1. The normal `bulle` parent parses CLI flags, loads config, resolves policy, prepares executable and library grants, and validates the backend.
2. If neither timeout nor CPU limit is configured, the current direct backend path remains available.
3. If timeout or CPU limit is configured, the parent starts the hidden internal runner using the same `bulle` executable.
4. The parent serializes the prepared policy and runner options to a private inherited file descriptor.
5. The runner child reads the policy from that fd, closes the fd, applies the CPU resource limit when configured, applies the selected backend, and runs the command.
6. The parent waits for the runner, enforces any wall-clock timeout, observes CPU-limit termination when possible, forwards relevant termination status, and returns the appropriate exit code.

This keeps sandbox setup in the existing backend code while ensuring `RLIMIT_CPU` affects only the prepared runner and sandboxed command tree.

## Components

### CLI Options

Extend `internal/cli.Flags` with a raw CPU limit string:

```go
CPULimit string `name:"cpu-limit" placeholder:"DURATION" help:"Limit per-process CPU time using Go duration syntax such as 30s, 2m, or 1h30m; whole seconds only; 0 disables."`
```

Extend `internal/cli.Options` with a typed field:

```go
CPULimit time.Duration
```

`cli.Parse` should validate the flag after Kong parsing with `time.ParseDuration`.

Validation rules:

- Empty value means no CPU limit.
- `"0"` means no CPU limit.
- Positive parsed durations of at least `1s` are accepted when divisible by `time.Second`.
- Parsed duration less than zero is rejected.
- Parsed duration greater than zero but less than `1s` is rejected.
- Parsed duration with fractional-second precision is rejected.
- Parse errors are returned as CLI errors.

### Policy Model

Add `CPULimit time.Duration` to `policy.Policy`.

Add a JSON field to `policy.View`:

```go
CPULimit string `json:"cpu_limit,omitempty"`
```

The JSON value should be omitted when no CPU limit is configured and should use `duration.String()` when set. The summary policy output should include a CPU limit line when set.

### Resource Limit Helper

Add a small Unix-only helper package or supervisor-local helper responsible for applying CPU resource limits, for example `internal/limits`.

The helper should expose a narrow API:

```go
func ApplyCPULimit(limit time.Duration) error
```

Unix behavior:

- Return nil when `limit <= 0`.
- Convert the limit to whole seconds. The CLI parser should already have guaranteed whole-second granularity.
- Read the current `RLIMIT_CPU` with `unix.Getrlimit`.
- If the current hard limit is finite and lower than the requested soft limit, return a setup error because `bulle` cannot raise the inherited hard limit.
- Set `Cur` to the requested seconds.
- Set `Max` to `seconds + 1` when the inherited hard limit permits it; otherwise keep the inherited hard limit.
- Call `unix.Setrlimit(unix.RLIMIT_CPU, &unix.Rlimit{Cur: cur, Max: max})`.
- Return a setup error if `Setrlimit` fails, including the requested limit in the message.

Unsupported-platform behavior:

- Return a clear setup error when a positive CPU limit is requested.

The helper should not own CLI parsing, policy resolution, backend selection, timeout handling, or process-group termination.

### Supervisor Package

Extend the supervisor options with CPU limit support:

```go
type Options struct {
	Executable  string
	Timeout     time.Duration
	CPULimit    time.Duration
	GracePeriod time.Duration
	Stdin       *os.File
	Stdout      *os.File
	Stderr      *os.File
}
```

The supervisor should allow `Run` when at least one resource control is configured:

- `Timeout > 0`
- `CPULimit > 0`

When only `CPULimit` is configured, the supervisor does not start a wall-clock timer. It waits for the runner and maps CPU-limit termination when observable.

Add a typed CPU-limit error:

```go
type CPULimitError struct {
	Duration time.Duration
}
```

The supervisor should return `CPULimitError` when the runner exits because of `SIGXCPU`, or when the runner exits because of the CPU hard-limit signal in a case the supervisor can confidently attribute to the configured CPU limit. Other command exits and signals should continue through the existing exit-status mapping.

The supervisor should continue to own:

- Creating an anonymous pipe for policy handoff.
- Starting the internal runner child with the read end inherited.
- Creating a process group for the runner and its descendants.
- Starting and enforcing the timeout timer when configured.
- Waiting for the runner to exit.
- Restoring foreground terminal ownership.
- Returning typed timeout and CPU-limit errors for `internal/app.Run` to map.

### Internal Runner

Keep the hidden internal invocation mode:

```text
bulle __run-prepared-policy --policy-fd 3
```

Runner behavior with CPU limits:

- Read a JSON payload from the provided fd.
- Decode the prepared `policy.Policy`.
- Close the fd before applying sandbox rules.
- Apply `CPULimit` with the resource-limit helper when set.
- Resolve the backend by name.
- Call `backend.Run(policy)`.
- Return the same exit-code mapping used by the normal app path.

The CPU limit should be applied after the policy fd is closed and before the backend starts the sandboxed command. This ensures the sandboxed command and backend helper process inherit the limit, while the private policy fd is already gone.

### Backend Changes

The backend interface can remain:

```go
type Backend interface {
	Run(policy.Policy) error
}
```

The Linux backend can continue to apply Landlock and call `syscall.Exec` inside the runner process. The exec replaces the runner with the target command, and the target command inherits `RLIMIT_CPU`.

The macOS backend can continue to generate a Seatbelt profile and run `/usr/bin/sandbox-exec`. The `sandbox-exec` process and its descendants inherit `RLIMIT_CPU` from the runner.

Existing `closeUnexpectedFileDescriptors` behavior must remain intact. The runner must close the policy fd before applying the CPU limit and calling the backend so the sandboxed command does not inherit a policy secret.

## Data Flow

Normal command without timeout or CPU limit:

```text
main -> app.Run -> cli.Parse -> config load -> policy.Resolve -> PreparePolicy -> backend.Run
```

Command with CPU limit and no timeout:

```text
main parent
  -> app.Run
  -> cli.Parse
  -> config load
  -> policy.Resolve
  -> PreparePolicy
  -> supervisor.Run
     -> start "bulle __run-prepared-policy --policy-fd 3"
     -> write prepared policy to fd
     -> wait for runner

runner child
  -> decode prepared policy
  -> close policy fd
  -> set RLIMIT_CPU
  -> backend.Run
  -> target command
```

Command with both timeout and CPU limit follows the same supervised path, with the parent also running the timeout timer.

## Error Handling

Parsing errors:

- Invalid `--cpu-limit` values return `ExitConfigError`.
- Error messages go to stderr and should include the invalid value.

Supervisor startup errors:

- Failure to create the policy pipe, start the runner, or configure process groups returns `ExitSandboxSetup`.

Runner decode errors:

- Invalid or missing policy fd returns `ExitSandboxSetup`.
- JSON decode failure returns `ExitSandboxSetup`.

Resource limit setup errors:

- Failure to apply `RLIMIT_CPU` returns `ExitSandboxSetup`.
- The error message should mention `--cpu-limit` and the requested duration.

Command errors:

- Existing command error mapping remains unchanged.
- The parent forwards the runner's non-timeout, non-resource-limit exit status according to the existing backend behavior.

CPU limit errors:

- CPU limit exhaustion returns `125`.
- The CPU-limit message is written by the parent supervisor when the parent can identify the resource-limit termination.

Timeout errors:

- Timeout behavior remains unchanged and returns `124`.

Terminal restoration errors:

- The supervisor should attempt terminal restoration even if process termination or wait returns an error.
- If the command did not time out or exceed the CPU limit and terminal restoration fails, return `ExitSandboxSetup`.
- If the command timed out or exceeded the CPU limit and terminal restoration also fails, keep the limit-specific exit code but include the restoration error on stderr after the limit message.

## Safety Requirements

- CPU limit parsing must reject values that cannot be represented as whole seconds.
- The CPU resource limit is applied only inside the hidden runner path, not to the public `bulle` parent.
- The prepared policy is never passed through argv.
- The prepared policy fd is closed before applying backend sandbox rules.
- The sandboxed command must not inherit unexpected fds.
- The parent always waits for the runner to avoid zombies.
- The terminal foreground process group is restored after normal exit, command failure, signal interruption, timeout, and CPU-limit termination.
- Omitted CPU limit follows the current direct execution path unless timeout is configured.
- `--timeout` and `--cpu-limit` can be used together without changing either option's documented meaning.
- The hidden runner mode is not documented as public API and should reject normal user-facing flags.

## Documentation

Update `internal/cli/usage.go` to include `--cpu-limit DURATION` under "Output and safety", near `--timeout`.

Regenerate generated docs with the existing docs command so `docs-src/cli-reference.md` and generated `docs/` output stay in sync with `bulle --help`.

The CLI reference should explain:

- The syntax uses Go duration values.
- Values must be whole seconds.
- `0` disables the CPU limit.
- The limit is per-process CPU time, not wall-clock time and not aggregate process-tree CPU accounting.
- Commands that exceed the CPU limit exit `125` when `bulle` can identify the resource-limit termination.

The risk-mitigation page should be updated once the feature ships. It should distinguish this per-process CPU-time cap from stronger resource isolation that still requires a separate machine, container, cgroup, or VM boundary.

## Testing

### Unit Tests

Add parser tests in `internal/cli/parse_test.go`:

- `--cpu-limit 30s` parses to `30 * time.Second`.
- `--cpu-limit=1h30m` parses to `90 * time.Minute`.
- omitted CPU limit parses to zero.
- `--cpu-limit 0` parses to zero.
- `--cpu-limit 30` is rejected.
- `--cpu-limit -1s` is rejected.
- `--cpu-limit 500ms` is rejected.
- `--cpu-limit 1.5s` is rejected.
- malformed values are rejected with a useful error.

Add usage tests:

- Help output contains `--cpu-limit DURATION`.
- Help output mentions Go-style duration examples.
- Help output mentions whole-second granularity.
- Help output mentions per-process CPU time.
- Help output mentions CPU-limit exit code `125`.

Add policy view tests:

- JSON policy output omits `cpu_limit` when zero.
- JSON policy output includes `"cpu_limit":"30s"` when configured.
- Summary policy output includes a CPU limit line when configured.

Add resource-limit helper tests through subprocess helpers so the test process does not lower its own CPU limit:

- Positive whole-second limit calls the Unix helper successfully in a child process.
- Zero limit is a no-op.
- Unsupported-platform stub returns a setup error for positive limits.

Add supervisor unit tests with helper commands:

- CPU-bound runner exceeds `--cpu-limit` and returns the typed CPU-limit error.
- Sleeping runner with a CPU limit exits normally.
- Command failure before CPU exhaustion preserves normal command-failure behavior.
- Timeout still returns the typed timeout error when wall-clock timeout fires before CPU exhaustion.
- CPU-limit termination restores the foreground terminal when terminal handling is active.

### Integration Tests

Linux integration:

- `bulle --cpu-limit 1s ... -- /usr/bin/yes` exits `125` when the CPU limit is observed.
- `bulle --cpu-limit 1s ... -- /bin/sleep 2` exits normally because sleeping does not consume CPU time.
- `bulle --timeout 100ms --cpu-limit 10s ... -- /bin/sleep 5` exits `124`.
- Existing Landlock denial tests still pass.

macOS integration:

- Equivalent CPU-bound command exits `125` when the CPU limit is observed.
- Sleeping command with a CPU limit exits normally.
- Existing Seatbelt denial tests still pass.
- Existing terminal ioctl regression test still passes.

Cross-platform behavior:

- `--cpu-limit 0` behaves like no CPU limit.
- `--policy=json --cpu-limit 30s -- ...` exits without running the command and includes the CPU limit in policy JSON.
- CPU-limit message appears on stderr when the parent can identify CPU-limit exhaustion.

## Open Decisions

None. This design uses portable per-process `RLIMIT_CPU` enforcement and explicitly leaves aggregate process-tree CPU accounting out of scope.
