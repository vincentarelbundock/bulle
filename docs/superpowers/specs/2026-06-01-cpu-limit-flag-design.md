# CPU Limit Flag Design

## Summary

Add a `--cpu-limit PERCENT` CLI flag that throttles the sandboxed command so it uses no more than a caller-specified average CPU load. This is a CPU utilization limit, not a CPU-time timeout. A CPU-bound command should continue running more slowly instead of being killed for consuming CPU.

The limit is expressed as a percentage of one logical CPU:

- `50%` means half of one logical CPU.
- `100%` means one logical CPU.
- `250%` means two and a half logical CPUs.

Omitted CPU limit, `--cpu-limit 0`, and `--cpu-limit 0%` preserve current behavior and do not start CPU throttling.

The implementation uses a supervisor-owned CPU governor. The governor periodically measures aggregate CPU time for the supervised process group, suspends the process group when it is ahead of its CPU budget, and resumes it when the budget recovers. This gives `bulle` a portable Linux and macOS implementation without treating CPU load as a timeout and without depending on external tools.

## Goals

- Provide a CPU-load cap for sandboxed commands on Linux and macOS.
- Throttle CPU-bound commands instead of killing them for using CPU.
- Express the limit in a user-facing syntax that clearly means utilization, not duration.
- Apply the limit to the supervised process group as a whole, not separately to each process.
- Preserve normal behavior when no CPU limit is configured.
- Preserve existing exit codes for successful commands, failed commands, missing executables, and non-executable paths.
- Keep CPU throttling inside `bulle`; do not depend on shell wrappers, `cpulimit`, `nice`, `renice`, `ulimit`, cgroups, systemd, launchd jobs, or external tools.
- Keep CPU throttling compatible with `--timeout`, interactive commands, TUIs, and existing signal forwarding.

## Non-Goals

- No CPU-time budget or CPU-time timeout in this change.
- No `RLIMIT_CPU` enforcement.
- No CPU priority-only feature; `nice`/`renice` are not load caps.
- No kernel-enforced cgroup/container CPU quota in this change.
- No memory, disk, process-count, or file-descriptor quotas in this change.
- No per-profile CPU defaults in config files.
- No user-configurable governor period, sampling interval, or control algorithm.
- No hard real-time scheduling guarantee. The cap is an average over short windows and can have brief bursts.
- No changes to sandbox permission semantics.

## User Interface

Add a top-level flag:

```text
--cpu-limit PERCENT
                  throttle the sandboxed command to an average CPU load
```

Examples:

```text
bulle --cpu-limit 50% --add-exec -- /usr/bin/yes
bulle --cpu-limit 100% ~/repos/project --profile codex -- codex
bulle --timeout 10m --cpu-limit 150% . -- claude
```

Accepted values:

- `0`: no CPU limit.
- `0%`: no CPU limit.
- Whole-number percentages from `1%` through `N*100%`, where `N` is the number of logical CPUs visible to `bulle`.

Rejected values:

- Unitless nonzero values, such as `50`.
- Negative values, such as `-10%`.
- Fractional values, such as `12.5%`.
- Values greater than the visible logical CPU capacity, such as `900%` on an eight-CPU machine.
- Malformed values, such as `half` or `50 percent`.

The parser error should name the invalid value and state that `--cpu-limit` expects a whole-number percentage such as `50%`, `100%`, or `250%`.

`--cpu-limit` does not apply in immediate non-command modes because they do not start a sandboxed command:

- `help`
- `version`
- `--list-profiles`
- `--install-profiles`
- `--policy`

Policy output should still include the CPU limit when one is configured so callers can inspect the resolved invocation before running it.

## CPU Limit Semantics

`--cpu-limit` limits average CPU utilization, not wall-clock duration and not accumulated CPU time. A command that saturates CPU should make slower progress. A command that mostly sleeps or waits for I/O should usually be unaffected.

The percentage is relative to one logical CPU. On an eight-CPU machine:

- `50%` allows roughly `0.5` CPU worth of average work.
- `100%` allows roughly `1` CPU worth of average work.
- `400%` allows roughly `4` CPUs worth of average work.

The limit applies to the supervised process group. If the command starts several CPU-bound child processes in the same process group, their CPU usage is summed and throttled against one shared budget.

This feature is best-effort. It uses sampling and `SIGSTOP`/`SIGCONT` throttling, so very short bursts can exceed the configured percentage before the next sample. The implementation should keep the sampling interval short enough that sustained load converges near the requested limit without making interactive commands unusably choppy.

Commands that deliberately move work into another process group or session can escape process-group throttling. This is the same process-boundary class that affects process-group signal handling generally; preventing deliberate escape requires a stronger execution boundary than this change provides.

## Exit Behavior

CPU limiting does not introduce a "CPU limit exceeded" exit code because exceeding the budget is handled by throttling.

When the command completes while CPU limiting is active, `bulle` preserves current exit behavior:

- `0` when the sandboxed command succeeds.
- Nonzero command exits keep the same observable behavior as they have without this feature.
- `126` when the command exists but is not executable.
- `127` when the command cannot be found.
- Existing setup and validation exit codes remain unchanged.

When `--timeout` and `--cpu-limit` are configured together:

- CPU limiting slows the command while it runs.
- Wall-clock timeout still exits `124` if the command runs longer than the configured timeout.
- Normal command exit before timeout preserves normal command exit behavior.

CPU throttling should not print routine stderr messages. Setup failures should print normal setup errors.

## Architecture

CPU throttling requires a parent process that remains outside the throttled process group. The timeout design already creates a parent supervisor and hidden prepared-policy runner. Reuse that architecture whenever either timeout or CPU limit is configured.

Use a shared supervised path:

1. The normal `bulle` parent parses CLI flags, loads config, resolves policy, prepares executable and library grants, and validates the backend.
2. If neither timeout nor CPU limit is configured, the current direct backend path remains available.
3. If timeout or CPU limit is configured, the parent starts the hidden internal runner using the same `bulle` executable.
4. The parent serializes the prepared policy to a private inherited file descriptor.
5. The runner child reads the policy from that fd, closes the fd, applies the selected backend, and runs the command.
6. The parent supervisor waits for the runner, enforces any wall-clock timeout, and runs the CPU governor when configured.

The CPU governor lives in the supervisor parent, not in the runner. This keeps the governor outside the sandbox, outside the throttled process group, and able to resume, terminate, or reap the runner even while the sandboxed command is stopped.

## Components

### CLI Options

Extend `internal/cli.Flags` with a raw CPU limit string:

```go
CPULimit string `name:"cpu-limit" placeholder:"PERCENT" help:"Throttle the sandboxed command to an average CPU load, such as 50%, 100%, or 250%; 0 disables."`
```

Extend `internal/cli.Options` with a typed field:

```go
CPULimitPercent int
```

`cli.Parse` should validate the flag after Kong parsing.

Validation rules:

- Empty value means no CPU limit.
- `"0"` and `"0%"` mean no CPU limit.
- Accepted positive values must end in `%`.
- Accepted positive values must be whole-number percentages.
- Accepted positive values must be at least `1%`.
- Accepted positive values must not exceed `runtime.NumCPU() * 100`.
- Parse errors are returned as CLI errors.

### Policy Model

Add `CPULimitPercent int` to `policy.Policy`.

Add a JSON field to `policy.View`:

```go
CPULimit string `json:"cpu_limit,omitempty"`
```

The JSON value should be omitted when no CPU limit is configured and should use the normalized percentage string when set, such as `"50%"` or `"250%"`. The summary policy output should include a CPU limit line when set.

### CPU Governor

Add a supervisor-owned CPU governor responsible for throttling one process group. A suitable boundary is `internal/supervisor`, with platform-specific CPU accounting helpers.

The governor API can be private to the supervisor:

```go
type cpuGovernor struct {
	pgid         int
	limitPercent int
	period       time.Duration
	sample       cpuSampler
	kill         func(int, syscall.Signal) error
}
```

Default timing:

- Use a fixed governor period of `100ms`.
- Keep this period internal for the first implementation.
- If tests show interactive commands are too choppy, tune the fixed value before exposing configuration.

Budget calculation:

- Convert `limitPercent` into allowed CPU time per wall-clock interval.
- For each sample interval, allowed CPU is `elapsed * limitPercent / 100`.
- CPU usage is the aggregate CPU-time delta for all processes in the supervised process group.
- If aggregate usage is within budget, leave the process group running.
- If aggregate usage is ahead of budget, send `SIGSTOP` to the process group and keep it stopped long enough for the wall-clock budget to catch up.
- Send `SIGCONT` before the next running interval.

The governor must not treat high CPU usage as an error. It only changes the process group's running/stopped state.

### CPU Accounting

Add a narrow platform-specific CPU sampler:

```go
type cpuSampler interface {
	SampleProcessGroup(pgid int) (cpuUsage, error)
}
```

`cpuUsage` should represent aggregate CPU time for processes currently in the process group. It can be an internal duration-like value.

Linux sampler:

- Enumerate processes under `/proc`.
- Include processes whose process-group id matches the supervised `pgid`.
- Read user and system CPU ticks from `/proc/<pid>/stat`.
- Convert ticks to duration using the platform clock tick rate.
- Parse `/proc/<pid>/stat` carefully because process names can contain spaces and parentheses.
- Tolerate processes exiting during a sample.

macOS sampler:

- Enumerate processes in the process group with the platform `sysctl` process APIs available through `golang.org/x/sys/unix`.
- Sum exported CPU accounting fields from `KinfoProc`/`ExternProc`.
- Tolerate processes exiting during a sample.

Sampler errors:

- If the sampler cannot enumerate the process group before the command exits, finish normally according to the runner exit status.
- If sampling fails while the command is still running, stop CPU limiting, resume the process group, and return `ExitSandboxSetup`.
- Error messages should mention `--cpu-limit` and the platform accounting failure.

Short-lived processes:

- Processes that start and exit between samples can be undercounted.
- This is acceptable for the first implementation and should be documented as part of best-effort throttling.
- The test suite should include sustained CPU load rather than relying on short micro-bursts.

### Supervisor Package

Extend supervisor options with CPU limit support:

```go
type Options struct {
	Executable      string
	Timeout         time.Duration
	CPULimitPercent int
	GracePeriod     time.Duration
	Stdin           *os.File
	Stdout          *os.File
	Stderr          *os.File
}
```

The supervisor should allow `Run` when at least one control is configured:

- `Timeout > 0`
- `CPULimitPercent > 0`

When only `CPULimitPercent` is configured, the supervisor does not start a wall-clock timeout timer. It waits for the runner while the CPU governor throttles the process group.

The supervisor continues to own:

- Creating an anonymous pipe for policy handoff.
- Starting the internal runner child with the read end inherited.
- Creating a process group for the runner and its descendants.
- Starting and enforcing the timeout timer when configured.
- Starting and stopping the CPU governor when configured.
- Forwarding relevant signals to the process group.
- Waiting for the runner to exit.
- Restoring foreground terminal ownership.

### Internal Runner

Keep the hidden internal invocation mode:

```text
bulle __run-prepared-policy --policy-fd 3
```

Runner behavior does not need CPU-limit-specific setup:

- Read a JSON payload from the provided fd.
- Decode the prepared `policy.Policy`.
- Close the fd before applying sandbox rules.
- Resolve the backend by name.
- Call `backend.Run(policy)`.
- Return the same exit-code mapping used by the normal app path.

The CPU limit is enforced by the parent supervisor, so the runner should not call `setrlimit`, `nice`, or any external throttling tool.

### Backend Changes

The backend interface can remain:

```go
type Backend interface {
	Run(policy.Policy) error
}
```

The Linux backend can continue to apply Landlock and call `syscall.Exec` inside the runner process.

The macOS backend can continue to generate a Seatbelt profile and run `/usr/bin/sandbox-exec`.

Existing `closeUnexpectedFileDescriptors` behavior must remain intact. The runner must close the policy fd before calling the backend so the sandboxed command does not inherit a policy secret.

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
     -> start CPU governor for runner process group
     -> wait for runner
     -> stop CPU governor and ensure process group is resumed

runner child
  -> decode prepared policy
  -> close policy fd
  -> backend.Run
  -> target command
```

Command with both timeout and CPU limit follows the same supervised path, with the parent also running the timeout timer.

## Signals and Terminal Handling

The CPU governor uses `SIGSTOP` and `SIGCONT` to throttle the process group. This interacts with the existing timeout supervisor's process-group and terminal handling.

Required behavior:

- Never leave the runner process group stopped after `bulle` exits.
- Send `SIGCONT` to the process group before returning from supervisor cleanup.
- If timeout fires while the process group is stopped, resume the process group as part of termination so cooperative signal handlers can run.
- If a forwarded signal such as `Ctrl-C` arrives while the process group is stopped, forward the signal and resume the process group.
- Stop the CPU governor before restoring terminal foreground process-group ownership.
- Restore the original terminal foreground process group after normal exit, command failure, signal interruption, and timeout.

If stdin is not a terminal, the governor still uses process-group throttling and skips foreground terminal handling.

## Error Handling

Parsing errors:

- Invalid `--cpu-limit` values return `ExitConfigError`.
- Error messages go to stderr and should include the invalid value.

Supervisor startup errors:

- Failure to create the policy pipe, start the runner, configure process groups, or start the CPU sampler returns `ExitSandboxSetup`.

Runner decode errors:

- Invalid or missing policy fd returns `ExitSandboxSetup`.
- JSON decode failure returns `ExitSandboxSetup`.

CPU governor errors:

- Sampling failure while the command is still running returns `ExitSandboxSetup`.
- Before returning a governor error, the supervisor must send `SIGCONT` to the process group and reap the runner or terminate it through the existing timeout-style cleanup path.

Command errors:

- Existing command error mapping remains unchanged.
- The parent forwards the runner's non-timeout exit status according to the existing backend behavior.

Timeout errors:

- Timeout behavior remains unchanged and returns `124`.
- CPU limiting does not change the timeout message or timeout exit code.

Terminal restoration errors:

- The supervisor should attempt terminal restoration even if CPU governor cleanup, process termination, or wait returns an error.
- If terminal restoration fails, preserve the existing timeout behavior for timeout cases and return `ExitSandboxSetup` for non-timeout setup/cleanup failures.

## Safety Requirements

- CPU limit parsing must reject duration syntax. `--cpu-limit 30s` is invalid.
- CPU throttling must not use `RLIMIT_CPU`.
- CPU throttling must not kill a command solely for using CPU.
- The CPU governor must run outside the throttled process group.
- The governor must always attempt to resume the process group before returning.
- The prepared policy is never passed through argv.
- The prepared policy fd is closed before backend sandbox setup.
- The sandboxed command must not inherit unexpected fds.
- The parent always waits for the runner to avoid zombies.
- The terminal foreground process group is restored after normal exit, command failure, signal interruption, and timeout.
- Omitted CPU limit follows the current direct execution path unless timeout is configured.
- `--timeout` and `--cpu-limit` can be used together without changing either option's documented meaning.
- The hidden runner mode is not documented as public API and should reject normal user-facing flags.

## Documentation

Update `internal/cli/usage.go` to include `--cpu-limit PERCENT` under "Output and safety", near `--timeout`.

Regenerate generated docs with the existing docs command so `docs-src/cli-reference.md` and generated `docs/` output stay in sync with `bulle --help`.

The CLI reference should explain:

- `--cpu-limit` accepts whole-number percentages such as `50%`, `100%`, and `250%`.
- Percentages are relative to one logical CPU.
- `0` and `0%` disable CPU limiting.
- CPU limiting throttles average load; it is not a timeout and does not add a "limit exceeded" exit code.
- `--timeout` remains the wall-clock kill switch.
- The implementation is best-effort and may allow short bursts between samples.

The risk-mitigation page should be updated once the feature ships. It should distinguish this best-effort local load cap from stronger resource isolation that requires a container, cgroup delegation, VM, or separate machine boundary.

## Testing

### Unit Tests

Add parser tests in `internal/cli/parse_test.go`:

- `--cpu-limit 50%` parses to `50`.
- `--cpu-limit=250%` parses to `250`.
- omitted CPU limit parses to zero.
- `--cpu-limit 0` parses to zero.
- `--cpu-limit 0%` parses to zero.
- `--cpu-limit 50` is rejected.
- `--cpu-limit 30s` is rejected.
- `--cpu-limit -1%` is rejected.
- `--cpu-limit 12.5%` is rejected.
- values over `runtime.NumCPU() * 100` are rejected.
- malformed values are rejected with a useful error.

Add usage tests:

- Help output contains `--cpu-limit PERCENT`.
- Help output mentions percentage examples.
- Help output says percentages are relative to one logical CPU.
- Help output says CPU limiting throttles load rather than killing on CPU use.

Add policy view tests:

- JSON policy output omits `cpu_limit` when zero.
- JSON policy output includes `"cpu_limit":"50%"` when configured.
- Summary policy output includes a CPU limit line when configured.

Add CPU governor unit tests with fake samplers and fake signal senders:

- Governor leaves the process group running while usage is under budget.
- Governor sends `SIGSTOP` when usage is ahead of budget.
- Governor sends `SIGCONT` after stopped debt clears.
- Governor sends `SIGCONT` during cleanup if the process group is stopped.
- Governor returns a setup error when sampling fails while the process is still running.

Add platform sampler tests:

- Linux `/proc/<pid>/stat` parsing handles command names with spaces and parentheses.
- Linux sampler tolerates processes exiting during enumeration.
- macOS sampler groups only processes with the requested process-group id.
- Sampler tests avoid lowering resource limits or relying on external tools.

Add supervisor unit tests with helper commands:

- CPU-bound runner is throttled and still exits normally when externally stopped by test logic.
- Sleeping runner with a CPU limit is not repeatedly stopped.
- Command failure while CPU limiting is active preserves normal command-failure behavior.
- Timeout still returns the typed timeout error when wall-clock timeout fires while CPU limiting is active.
- CPU governor cleanup resumes a stopped process group before timeout termination.
- Terminal foreground process group is restored while CPU limiting is active.

### Integration Tests

Linux integration:

- A CPU-bound command under `--cpu-limit 50%` consumes substantially less CPU over a multi-second window than the same command without the limit.
- A command that starts multiple CPU-bound children in the same process group is throttled against one shared budget.
- `bulle --timeout 100ms --cpu-limit 50% ... -- /bin/sleep 5` exits `124`.
- Existing Landlock denial tests still pass.

macOS integration:

- Equivalent CPU-bound command is throttled over a multi-second window.
- A command that starts multiple CPU-bound children in the same process group is throttled against one shared budget.
- Existing Seatbelt denial tests still pass.
- Existing terminal ioctl regression test still passes.

Cross-platform behavior:

- `--cpu-limit 0` and `--cpu-limit 0%` behave like no CPU limit.
- `--policy=json --cpu-limit 50% -- ...` exits without running the command and includes the CPU limit in policy JSON.
- `--cpu-limit` does not print routine stderr messages while throttling.

## Open Decisions

None. This design limits CPU load by throttling the supervised process group and explicitly avoids CPU-time timeout semantics.
