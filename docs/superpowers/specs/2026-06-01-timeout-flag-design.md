# Timeout Flag Design

## Summary

Add a `--timeout DURATION` CLI flag that stops the sandboxed command after a caller-specified wall-clock duration. The duration uses Go syntax accepted by `time.ParseDuration`, such as `30s`, `2m`, or `1h30m`. Omitted timeout and `--timeout 0` preserve current behavior and do not start timeout supervision.

The implementation must work on Linux and macOS, must terminate the whole sandboxed command tree when the deadline expires, and must not weaken existing sandbox setup, file descriptor cleanup, terminal behavior, or command exit-code conventions.

## Goals

- Provide a portable timeout for sandboxed commands on Linux and macOS.
- Accept explicit Go-style duration values with units.
- Reject ambiguous or unsafe timeout values before sandbox setup begins.
- Kill the sandboxed command and its descendants on timeout.
- Preserve normal behavior when no timeout is configured.
- Preserve existing exit codes for successful commands, failed commands, missing executables, and non-executable paths.
- Keep timeout handling inside `bulle`; do not depend on external tools such as GNU `timeout`.
- Keep the timeout implementation compatible with interactive commands and TUIs.

## Non-Goals

- No CPU, memory, or disk quotas in this change.
- No idle timeout based on stdout, stderr, stdin, or filesystem activity.
- No per-profile timeout defaults in config files.
- No user-configurable signal sequence or grace period.
- No changes to sandbox permission semantics.

## User Interface

Add a top-level flag:

```text
--timeout DURATION
                  kill the sandboxed command if it runs longer than DURATION
```

Examples:

```text
bulle --timeout 30s --add-exec -- /bin/sleep 60
bulle --timeout 2m ~/repos/project --profile codex -- codex
bulle --timeout 1h30m . -- claude
```

Accepted values:

- `0`: no timeout.
- Any positive duration accepted by `time.ParseDuration`, including combined values such as `1h30m`.

Rejected values:

- Negative durations, such as `-1s`.
- Unitless nonzero values, such as `30`.
- Malformed duration strings, such as `ten seconds`.

The parser error should name the invalid value and state that `--timeout` expects a Go duration such as `30s`, `2m`, or `1h30m`.

`--timeout` does not start timeout supervision in immediate non-command modes because they do not start a sandboxed command:

- `help`
- `version`
- `--list-profiles`
- `--install-profiles`
- `--policy`

Policy output should still include the timeout value when one is configured so callers can inspect the resolved invocation before running it.

## Exit Behavior

When the command completes before the timeout, `bulle` preserves current exit behavior for the active execution path:

- `0` when the sandboxed command succeeds.
- Nonzero command exits keep the same observable behavior as they have without this feature; timeout work must not normalize command failures as a side effect.
- `126` when the command exists but is not executable.
- `127` when the command cannot be found.
- Existing setup and validation exit codes remain unchanged.

When the timeout expires, `bulle` returns exit code `124`. This matches the conventional timeout-wrapper status used by GNU `timeout` and avoids conflict with the existing shell-style meanings of `126` and `127`.

On timeout, stderr prints one line:

```text
bulle: command timed out after 30s
```

The duration in the message should be the normalized Go duration string for the parsed value.

## Architecture

Timeout enforcement requires a parent process that can observe and kill the sandboxed command. The current macOS backend already runs a child process through `exec.Command`, but the Linux backend currently calls `syscall.Exec`, replacing the `bulle` process with the target command. Linux therefore needs a new supervised path.

Use a shared supervisor architecture for both platforms:

1. The normal `bulle` parent parses CLI flags, loads config, resolves policy, prepares executable and library grants, and validates the backend.
2. If no timeout is configured, the current direct backend path remains available.
3. If a timeout is configured, the parent starts a hidden internal runner child using the same `bulle` executable.
4. The parent serializes the prepared policy and runner options to a private inherited file descriptor.
5. The runner child reads the policy from that fd, closes the fd, applies the selected backend, and runs the command.
6. The parent waits for the runner, enforces the timeout, forwards relevant termination status, and returns `124` if the deadline expires.

This keeps sandbox setup in the existing backend code while giving Linux the parent process it needs for timeout enforcement.

## Components

### CLI Options

Extend `internal/cli.Flags` with a timeout field that stores the raw CLI value or parsed duration. The public `cli.Options` value should expose a typed `time.Duration` so later code does not need to repeatedly parse strings.

Kong can accept the flag as a string. `cli.Parse` should validate it after Kong parsing with `time.ParseDuration`.

Validation rules:

- Empty value means no timeout.
- `"0"` means no timeout.
- Positive parsed duration is accepted.
- Parsed duration less than zero is rejected.
- Parse errors are returned as CLI errors.

### Policy Model

Add `Timeout time.Duration` to `policy.Policy`.

Add a JSON field to `policy.View`:

```go
Timeout string `json:"timeout,omitempty"`
```

The JSON value should be omitted when no timeout is configured and should use `duration.String()` when set. The summary policy output should include a timeout line when set.

### Supervisor Package

Add a small internal component responsible for supervised command execution. A suitable boundary is `internal/supervisor`, with one public function used by `internal/app`:

```go
func Run(policy.Policy, Options) error
```

The supervisor should own:

- Creating an anonymous pipe for policy handoff.
- Starting the internal runner child with the read end inherited.
- Starting the timeout timer after the child starts.
- Creating a process group for the runner and its descendants.
- Killing the process group on timeout.
- Waiting for the runner to exit.
- Returning a typed timeout error for `internal/app.Run` to map to exit code `124`.

The supervisor should not own policy resolution, backend selection, or sandbox rule generation.

### Internal Runner

Add a hidden internal invocation mode that is not shown in help and is not intended for users. The parent invokes it with an argv marker and an inherited fd number, for example:

```text
bulle __run-prepared-policy --policy-fd 3
```

The marker should be recognized before normal CLI parsing, because it is not part of the public grammar.

Runner behavior:

- Read a JSON payload from the provided fd.
- Decode the prepared `policy.Policy`.
- Close the fd before applying sandbox rules.
- Resolve the backend by name.
- Call `backend.Run(policy)`.
- Return the same exit-code mapping used by the normal app path.

The policy payload includes environment values, so it must not be passed through argv, logs, or public policy output.

### Process Groups and Signals

The supervisor must terminate the whole command tree, not only the immediate runner process.

Required behavior:

- Start the runner in a new process group.
- On timeout, send `SIGTERM` to the negative process group id.
- Wait a short fixed grace period while also waiting for the runner to exit.
- After the grace period, send `SIGKILL` to the negative process group id unless the process group no longer exists.
- Reap the runner process before returning.

Use a fixed grace period of one second. This gives cooperative commands a brief chance to clean up while keeping timeout behavior predictable.

`SIGTERM` and `SIGKILL` are available on both Linux and macOS.

### Interactive Terminal Safety

Creating a separate process group can affect terminal job control. Interactive commands must still receive stdin and behave normally.

When stdin is a terminal, the supervisor must ensure the runner process group can use the terminal while it is running. The design should use platform-specific terminal foreground process-group handling, or another tested mechanism with equivalent behavior:

- Save the original foreground process group.
- Temporarily prevent terminal-control signals from stopping the parent while it updates the foreground process group.
- Put the runner process group in the foreground before waiting.
- Restore the original foreground process group after the runner exits or is killed.
- Do not leave the terminal attached to the runner process group after timeout.

Signal forwarding should preserve current user expectations:

- `Ctrl-C` should interrupt the sandboxed command.
- `Ctrl-\` should quit the sandboxed command.
- The `bulle` parent should not exit without reaping the runner.

If stdin is not a terminal, skip foreground terminal handling and still use process-group termination.

### Backend Changes

The backend interface can remain:

```go
type Backend interface {
	Run(policy.Policy) error
}
```

The Linux backend can continue to apply Landlock and call `syscall.Exec` inside the runner process. The exec replaces the runner with the target command, which is correct because the parent supervisor remains outside the sandbox and outside the runner process group.

The macOS backend can continue to create a Seatbelt profile and run `/usr/bin/sandbox-exec`. It should remain compatible with being called from the runner process.

Existing `closeUnexpectedFileDescriptors` behavior must remain intact. The runner must close the policy fd before calling the backend so the sandboxed command does not inherit a policy secret.

## Data Flow

Normal command without timeout:

```text
main -> app.Run -> cli.Parse -> config load -> policy.Resolve -> PreparePolicy -> backend.Run
```

Command with timeout:

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
     -> wait with timer

runner child
  -> decode prepared policy
  -> close policy fd
  -> backend.Run
  -> target command
```

## Error Handling

Parsing errors:

- Invalid `--timeout` values return `ExitConfigError`.
- Error messages go to stderr and should include the invalid value.

Supervisor startup errors:

- Failure to create the policy pipe, start the runner, or configure process groups returns `ExitSandboxSetup`.

Runner decode errors:

- Invalid or missing policy fd returns `ExitSandboxSetup`.
- JSON decode failure returns `ExitSandboxSetup`.

Command errors:

- Existing command error mapping remains unchanged.
- The parent forwards the runner's non-timeout exit status according to the existing backend behavior.

Timeout errors:

- Timeout returns `124`.
- Timeout message is written by the parent supervisor, not by the runner, so it appears even if the runner is already being killed.

Terminal restoration errors:

- The supervisor should attempt terminal restoration even if process termination or wait returns an error.
- If the command did not time out and terminal restoration fails, return `ExitSandboxSetup`.
- If the command timed out and terminal restoration also fails, return `124` but include the restoration error on stderr after the timeout message.

## Safety Requirements

- The timeout clock starts only after the runner child successfully starts.
- The prepared policy is never passed through argv.
- The prepared policy fd is closed before backend sandbox setup.
- The sandboxed command must not inherit unexpected fds.
- Timeout kills the process group, not just one process.
- The parent always waits for the runner to avoid zombies.
- The terminal foreground process group is restored after normal exit, command failure, signal interruption, and timeout.
- Omitted timeout follows the current direct execution path unless later implementation chooses to use the supervisor universally after equivalent behavior is proven.
- The hidden runner mode is not documented as public API and should reject normal user-facing flags.

## Documentation

Update `internal/cli/usage.go` to include `--timeout DURATION` under "Output and safety".

Regenerate generated docs with the existing docs command so `docs-src/cli-reference.md` and generated `docs/` output stay in sync with `bulle --help`.

The CLI reference should explain:

- The syntax uses Go duration values.
- `0` disables the timeout.
- Timed-out commands exit `124`.

## Testing

### Unit Tests

Add parser tests in `internal/cli/parse_test.go`:

- `--timeout 30s` parses to `30 * time.Second`.
- `--timeout=1h30m` parses to `90 * time.Minute`.
- omitted timeout parses to zero.
- `--timeout 0` parses to zero.
- `--timeout 30` is rejected.
- `--timeout -1s` is rejected.
- malformed values are rejected with a useful error.

Add usage tests:

- Help output contains `--timeout DURATION`.
- Help output mentions Go-style duration examples.
- Help output mentions timeout exit code `124`.

Add policy view tests:

- JSON policy output omits timeout when zero.
- JSON policy output includes `"timeout":"30s"` when configured.
- Summary policy output includes a timeout line when configured.

Add supervisor unit tests where possible with a fake runner command:

- Timeout returns the typed timeout error.
- Non-timeout child success returns nil.
- Non-timeout child failure returns an exit error.
- Supervised non-timeout command exits preserve the existing command-failure behavior.
- Timeout attempts process-group termination.

### Integration Tests

Linux integration:

- `bulle --timeout 100ms ... -- /bin/sleep 5` exits `124` and returns well before five seconds.
- A shell command that starts a background `sleep` is fully cleaned up on timeout.
- A command that exits before the timeout preserves exit code behavior.

macOS integration:

- Equivalent `sleep` timeout test exits `124`.
- A shell command that starts a background `sleep` is fully cleaned up on timeout.
- Existing Seatbelt denial tests still pass.
- Existing terminal ioctl regression test still passes.

Cross-platform behavior:

- `--timeout 0` behaves like no timeout.
- `--policy=json --timeout 30s -- ...` exits without running the command and includes the timeout in policy JSON.
- Timeout message appears on stderr.

## Open Decisions

None. The accepted design uses Go-style duration syntax and exit code `124` for timeout.
