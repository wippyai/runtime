<!-- SPDX-License-Identifier: MPL-2.0 -->

# exec

Command execution and process management. IO, process, nondeterministic.

## Loading

```lua
local exec = require("exec")
```

## Dependencies

### Stream (from stream module)

Returned by `process:stdout_stream()` and `process:stderr_stream()` for reading process output.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| read | (size?: integer) | string, error | Reads up to size bytes, yields until data available |
| close | () | boolean, error | Closes the stream |

See: `runtime/lua/modules/stream/` (no spec yet)

## Functions

### get(id: string) → Executor, error

Acquires a process executor resource by ID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Resource ID (e.g., "app:exec") |

**Returns:**
- Success: `Executor, nil` - executor object for creating processes
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| id is empty string | errors.INVALID | no |
| permission denied | errors.INVALID | no |
| resource not found | errors.INTERNAL | no |
| registry not available | errors.INTERNAL | no |
| resource wrong type | errors.INTERNAL | no |

**Example:**

```lua
local executor, err = exec.get("app:exec")
if err then error(err) end
```

## Types

### Executor

Returned by `exec.get()`. Creates and manages processes.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| exec | (cmd: string, options?: ProcessOptions) | Process, error | Creates a new process |
| terminal | (cmd: string, options?: ProcessOptions) | TerminalProcess, error | Starts a PTY process in the caller's terminal |
| release | () | boolean, error | Releases the executor resource |

#### executor:terminal(cmd: string, options?: ProcessOptions) → TerminalProcess, error

Creates and starts a host process whose PTY is rendered in the caller's current
terminal port. The call yields until the child has started, its initial size is
set, and PTY output is available. Startup or setup failures return `nil, error`
after cleanup; no partially started handle is returned. The returned
`TerminalProcess` is the sole lifecycle owner.

The same `exec.run` and `exec.mount` permissions and process options apply as
to `executor:exec`. A PTY is allocated automatically; `options.pty.term` may
select the child's `TERM`. The current terminal geometry supplies omitted PTY
dimensions and the process is resized to that geometry after startup.

```lua
local terminal = assert(executor:terminal("bash", {
    pty = {term = "xterm-256color"},
}))
local host_pid, pid_err = terminal:pid() -- optional host OS identity
-- Forward tty events with terminal:send(event).
local done = terminal:done()             -- terminal-finalization channel
local result = done:receive()
if result.exit then print(result.exit.code) end
if result.terminal_error then error(result.terminal_error) end
```

`TerminalProcess` exposes `send(event)`, `done()`, `pid()`, `status()`, and
`close()`. `pid()` has no startup-pending state on a returned object, but
returns a non-retryable unavailable error for executors without host PID
identity. `done()` carries one `TerminalResult` after the child has been waited
on, output drained, and the terminal finalized. `result.exit` has the child's
`code`, optional `signal`, and optional `error` if its exit could not be
observed. It is absent if the proxy could not reap the child. A non-zero code
is a child exit, not a terminal error. `result.terminal_error` separately
reports a proxy, I/O, or shutdown failure. `close()` hangs up the terminal:
the child is sent `SIGHUP`, which interactive shells honor and forward to their
jobs, then `SIGKILL` if it is still running after a grace period, and is
reaped; await `done()` to observe completion. Neither the PID
nor an exit result is an authorization grant or proof that a process group
exited.

#### executor:exec(cmd: string, options?: ProcessOptions) → Process, error

Creates a new process with the specified command.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| cmd | string | yes | - | Executable and literal arguments; supports single/double quotes and backslash escaping, but performs no shell expansion |
| options | ProcessOptions | no | nil | Process options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| work_dir | string | nil | Working directory for the process |
| env | {[string]: string} | nil | Environment variables as key-value map |
| pty | PTYOptions | nil | Allocate a pseudo-terminal for the child |
| process_group | boolean | executor default | Start the child in its own process group so signals reach descendants; unsupported on Windows and on confined native launches |
| mounts | Mount[] | nil | Bind host paths into the process; each mount requires `exec.mount` permission |
| confine | ConfinementPatch | nil | Narrow the native executor entry's confinement ceiling for this launch |

**Mount fields:**

| Field | Type | Default | Notes |
|---|---|---|---|
| source | string | required | Clean absolute host path checked by the separate mount permission |
| target | string | required | Absolute path inside the process |
| read_only | boolean | false | Mount the source read-only |

Docker supports these per-process bind mounts; native execution rejects them.
Each source is authorized with action `exec.mount`, resource equal to `source`,
and `target` and `read_only` attributes before process creation. This permission
checks the supplied path; it does not resolve symlinks or provide filesystem
confinement. The source refers to the Docker daemon's host. Duplicate targets
within the request or against configured Unix container bind targets are refused.
Process options are copied at creation so later caller changes cannot alter them.

The `exec.run` permission uses `cmd` as its resource. Its metadata contains
`executor` (the registry ID acquired with `exec.get`), `work_dir`, sorted
`env_names`, `process_group`, and `pty` (`requested`, `width`, `height`, and
`term`). The metadata describes the request; confinement is independently
validated and enforced by the executor.

```lua
local proc, err = exec.process("python worker.py", {
  work_dir = "/workspace",
  mounts = {
    { source = "/srv/data", target = "/workspace/data", read_only = true },
    { source = "/srv/out",  target = "/workspace/out" },
  },
})
-- refused with errors.PERMISSION_DENIED when any mount's source is not
-- allowed for exec.mount; details.source names the refused path
```

**ConfinementPatch fields:**

| Field | Type | Default | Notes |
|---|---|---|---|
| fs | ConfinementFS | inherit | Restrict filesystem read, write, and execute directory grants |
| env | ConfinementEnvironment | inherit | Restrict which caller-provided environment names are accepted |
| network | `"none"` | inherit | Deny socket creation and use, including `socketpair` |
| limits | ConfinementLimits | inherit | Narrow whole-tree memory, task, and wall-time ceilings |
| tree | ConfinementTree | inherit | Request descendant cleanup when the owning process ends |

`fs` contains `read`, `write`, and `exec` arrays of clean absolute directory
paths. `env` contains an `allow` array. `limits` contains positive integer
`mem_mb`, `pids`, and `wall_s` values. `tree` contains the boolean
`kill_on_owner_exit`.

`pids` is a task ceiling, matching Linux cgroup-v2 `pids.max`: every process
and every thread consumes one unit. A platform that can only limit processes
does not satisfy this guarantee, even when it otherwise confines the launch to
a single process.

Confinement is entry-owned. The `confine` block on an `exec.native` registry
entry defines the maximum authority available to all of its processes. A Lua
launch can only narrow that baseline; it cannot select new roots, restore
environment names, raise limits, re-enable networking, or weaken a requested
tree guarantee. `work_dir_roots`, `home`, and `env.set` are consequently
entry-only and are rejected in Lua options.

An omitted launch field inherits its entry value. Within `fs` and `env`, an
omitted list also inherits, while a present empty list denies the entire class.
Filesystem write permission implies read permission. If the entry filesystem
is unrestricted, a launch-time filesystem restriction must provide all three
classes because the current backend cannot express a partly unrestricted
view. When both read and write are explicit, every write path must also be
listed under read because write implies read. `{home}` and `{tmp}` refer only to the exact private roots declared by
the entry; they do not accept suffixes. The selected `work_dir` must remain
inside an entry-owned working-directory root and the final filesystem view.

The environment starts from the executor defaults plus caller values. A
confined entry may pin values with entry-only `env.set`; callers cannot replace
them. Other caller values must be named by the final `env.allow`. `HOME` and
temporary-directory variables are runtime-owned when private directories are
active and cannot be supplied by the caller.

Native confinement uses the same entry and Lua surface on every platform, but
each host must be able to enforce every requested guarantee. Unsupported
combinations fail with `CONFINE_UNSUPPORTED` before the target runs; they never
silently degrade.

| Guarantee | Linux | macOS | Windows |
|---|---|---|---|
| environment ceiling and entry-owned values | yes | yes | yes; Windows bootstrap/profile variables are platform-owned |
| private home | yes | yes | yes |
| filesystem policy | directory grants | fail closed | fail closed |
| `network: none` | total socket denial, including `socketpair` | fail closed | fail closed |
| aggregate memory limit | delegated cgroup v2 | fail closed | Job Object committed-memory limit |
| aggregate task limit | delegated cgroup v2 | fail closed | fail closed |
| wall timeout | whole process tree | singleton process domain | singleton Job domain |
| owner-exit cleanup | PID-namespace process tree | singleton process domain | singleton Job domain |
| runtime-crash cleanup | not promised | not promised | Job kill-on-close |
| confined PTY/process group | PTY only | fail closed | fail closed |

Linux filesystem grants are existing directories; individual-file grants are
not supported. Restricted views contain selected `/dev` nodes and no `/proc`,
and may need explicit read/execute grants for dynamic loaders and libraries.
Writable grants permit data changes but intentionally deny chmod, chown,
xattrs, timestamp mutation, and most ioctls; they are not full POSIX
filesystem authority. Filesystem restrictions require Landlock ABI 5, and
memory/task limits require delegated cgroup-v2 controllers.

macOS launches go through a separately signed Seatbelt trampoline which is
started suspended and verified before it can execute. Working directories are
selected through entry-owned descriptors, so replacing a configured root
cannot redirect a prepared launch. Seatbelt pathname rules cannot provide the
object-bound filesystem policy promised by Linux, and an unprivileged process
cannot provide aggregate memory/task controls or prove total socket denial.
Those requests therefore fail closed. Wall and owner-exit controls deny child
creation and operate on a singleton process domain. The dynamic Seatbelt
entry point used for arbitrary executables is deprecated by Apple; native CI
therefore validates the signed trampoline on every supported macOS release,
but the backend cannot claim a stable Apple SDK compatibility contract.

Windows launches use a fresh less-privileged AppContainer identity and a Job
Object. The runtime atomically assigns the suspended target to the Job and
verifies its exact package SID, single outbound-public-Internet capability,
low integrity, child-process restriction, singleton Job limits, and Job
membership before resuming it. Access for the unique launch SID is temporarily
granted only to runtime-controlled launch objects and is removed after the Job
is empty. The capability is not full inherited host networking: private,
inbound, and loopback access are not promised. Writable work directories must
be pre-provisioned with an inheritable
low mandatory-integrity `NO_WRITE_UP` label; the runtime validates this and
never silently relabels a host tree. Configured work roots must consequently be
dedicated trees whose ACL management and descendant placement remain under
Wippy's control for the launch; unrelated ACL writers are outside this host
contract. Filesystem policy blocks, total network denial, and task-count limits
fail closed. The
runtime supplies trusted `SYSTEMROOT` and `LOCALAPPDATA` bootstrap values;
Windows rewrites `LOCALAPPDATA`, `TEMP`, and `TMP` into the package-private
profile. These platform-managed names cannot be set or admitted by the entry
environment policy, and no other host environment is inherited implicitly.
Closing the runtime's non-inheritable Job handle kills the sandbox, including
when the runtime process terminates unexpectedly. Go 1.27 also has a Windows
runtime compatibility defect in which a failed AppContainer `WSAStartup` can
poison later `internal/poll` file operations. Native Win32 conformance tests
prove the ACL and integrity contract independently, but arbitrary Go payloads
that mix failed networking with later file I/O may remain affected by that Go
runtime defect.

Docker confinement is not implemented. There is no network allowlist/proxy,
CPU or I/O quota, persistent private home, launch-time widening, or
portable runtime-crash cleanup guarantee.

On Linux the current backend always destroys remaining descendants when the
root exits or is stopped. Therefore `kill_on_owner_exit = false` means that
this minimum guarantee was not requested; it does not request that descendants
survive on a backend which enforces stronger cleanup.

```lua
local proc = assert(executor:exec("/workspace/bin/job", {
  work_dir = "/workspace",
  env = { LANG = "C" },
  confine = {
    fs = {
      read = { "/workspace", "/usr/lib", "{tmp}" },
      write = { "{tmp}" },
      exec = { "/workspace", "/usr/lib" },
    },
    env = { allow = { "LANG" } },
    network = "none",
    limits = { mem_mb = 512, pids = 32, wall_s = 60 },
    tree = { kill_on_owner_exit = true },
  },
}))
```

**PTYOptions fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| width | integer | 80 | Initial PTY columns |
| height | integer | 24 | Initial PTY rows |
| term | string | nil | Child `TERM` value, such as `xterm-256color` |

**Returns:**
- Success: `Process, nil` - process object (not started)
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| executor released | errors.INVALID | no |
| cmd is empty string | errors.INVALID | no |
| cmd contains an unclosed quote | errors.INVALID | no |
| permission denied | errors.INVALID | no |
| process creation failed | errors.INTERNAL | no |
| malformed Lua confinement table | errors.INVALID | no |
| semantically invalid confinement | errors.INVALID (`CONFINE_INVALID`) | no |
| launch confinement would widen the entry ceiling | errors.INVALID (`CONFINE_WIDEN`) | no |
| working directory lies outside the entry ceiling | errors.PERMISSION_DENIED (`CONFINE_DENIED`) | no |
| platform or host cannot enforce the requested guarantee | errors.UNAVAILABLE (`CONFINE_UNSUPPORTED`) | no |
| confinement helper cannot install the admitted policy | errors.UNAVAILABLE (`CONFINE_SETUP`) | no |

**Example:**

```lua
local proc, err = executor:exec("echo hello", {
    work_dir = "/tmp",
    env = { MY_VAR = "value" }
})
if err then error(err) end
```

#### executor:release() → boolean, error

Releases the executor resource. Safe to call multiple times.

**Returns:** `true, nil` - always succeeds

**Example:**

```lua
executor:release()
```

### Process

Returned by `executor:exec()`. Represents a process instance.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| start | () | boolean, error | Starts the process |
| wait | () | integer, error | Waits for process to exit, consumes the handle, yields |
| done | () | ProcessExitChannel, error | One-shot channel carrying the exit; keeps the handle usable |
| signal | (sig: integer) | boolean, error | Sends signal to process |
| pid | () | integer, error | Returns the started child process ID |
| write_stdin | (data: string) | boolean, error | Writes to process stdin |
| stdout_stream | () | Stream, error | Returns stdout stream |
| stderr_stream | () | Stream, error | Returns stderr stream |
| resize | (width: integer, height: integer) | boolean, error | Resizes a PTY-backed process |
| close | (force?: boolean) | boolean, error | Signals the process, reaps it, releases the handle |

#### process:resize(width: integer, height: integer) → boolean, error

Resizes an allocated PTY on a regular `Process`. Ordinary pipe-backed
processes return an error. A `TerminalProcess` follows its terminal geometry
automatically.

#### process:close(force?: boolean) → boolean, error

Releases the process. A started child is sent `SIGTERM`, or `SIGKILL` when
`force` is true, then reaped; an unstarted handle is simply invalidated. A
process started with `process_group` is signaled as a group, so its descendants
go with it. The group outlives the child that leads it, so `close()` still
reaches the descendants when the child's own exit has already been observed
through `done()`.

Reaping is what releases the child's entry in the OS process table; without it a
stopped process lingers as a zombie for the lifetime of the runtime. It happens
in the background so `close()` does not block, and a child still running after a
grace period is killed so the reap always completes.

A stream taken from `stdout_stream()` or `stderr_stream()` outlives the reap:
the bytes the child wrote before it exited are still readable, and the stream
ends when the last writer closes the pipe, which may be a descendant rather than
the child itself.

After `close()` every method on the process, including `wait()`, reports
`process closed`. Use `done()` instead when the exit code matters and the
handle has to stay usable, or `wait()` when it does not.

**Returns:**
- Success: `true, nil`
- Already closed: `true, nil` - closing twice is not an error

#### process:start() → boolean, error

Starts the process. Must be called before other operations.

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| process closed | errors.INVALID | no |
| process already started | errors.INVALID | no |
| start failed | errors.INTERNAL | no |

**Example:**

```lua
local ok, err = proc:start()
if err then error(err) end
```

#### process:wait() → integer, error

Waits for the process to exit and returns the exit code.

`wait()` consumes the handle. The process is released the moment `wait()` is
called, before it yields: every other method, `wait()` included, reports
`process closed` from then on. Take the streams you need before calling it, and
use `done()` when the handle must stay usable; a stream already taken stays
readable through the exit.

A child killed by a signal has no exit code of its own; it is reported as
`128 + signal`, so `SIGTERM` becomes 143 and `SIGKILL` 137. That is not an
error: the error return is reserved for a failure to observe the exit at all.
The signal number itself is on the `done()` value.

`wait()` after `done()` has delivered the exit returns the recorded exit code
rather than failing. The child is waited on once, whichever of `done()`,
`wait()` and `close()` gets there first.

**Yields:** until process exits

**Returns:**
- Success: `exit_code: integer, nil` - exit code (0 for success, non-zero for failure)
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| process closed | errors.INVALID | no |
| process not started | errors.INVALID | no |
| wait failed | errors.INTERNAL | no |
| process error | errors.INTERNAL | no |

**Example:**

```lua
proc:start()
local exitCode, err = proc:wait()
if err then error(err) end
if exitCode ~= 0 then
    error("process failed with code " .. exitCode)
end
```

#### process:done() → ProcessExitChannel, error

Returns a channel that delivers the child's exit exactly once, then closes.
Unlike `wait()` it leaves the handle open: `write_stdin()`, `signal()`, the
streams and `close()` all keep working, so a supervisor can keep driving the
child while another coroutine waits for it to finish.

Draining stdout is not a substitute for this. A grandchild inherits the pipe
and can hold it open long after the child itself is gone, so an EOF that never
arrives says nothing about the exit.

Calling `done()` again returns the same channel. Because the channel closes
behind the single value, a second `receive()` reports `nil, false` instead of
blocking.

**The exit value:**

| Field | Type | Notes |
|-------|------|-------|
| code | integer | Exit code, or `128 + signal` for a child killed by a signal |
| signal | integer | Signal that killed the child; absent when it exited on its own |
| error | error | Set only when the exit could not be observed at all |

`done()` reaps the child as soon as it exits, and the streams survive that: what
the child wrote on its way out is still there to be read once the exit has been
delivered. The streams end on their own, when the last process holding the pipe
closes it.

**Returns:**
- Success: `channel, nil`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| process closed | errors.INVALID | no |
| process not started | errors.INVALID | no |
| process context unavailable | errors.UNAVAILABLE | no |

**Example:**

```lua
proc:start()
local exit = assert(proc:done())

coroutine.spawn(function()
    local status = channel.select{ exit:case_receive() }.value
    print("child exited with", status.code, status.signal)
end)

proc:write_stdin("work\n")
proc:signal(15)
```

#### process:signal(sig: integer) → boolean, error

Sends a signal to the running process.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sig | integer | yes | - | Signal number (e.g., 15 for SIGTERM, 9 for SIGKILL) |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| process closed | errors.INVALID | no |
| process not started | errors.INVALID | no |
| signal failed | errors.INTERNAL | no |

**Example:**

```lua
local SIGTERM = 15
proc:start()
local ok, err = proc:signal(SIGTERM)
if err then error(err) end
```

#### process:write_stdin(data: string) → boolean, error

Writes data to the process stdin.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Data to write to stdin |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| process closed | errors.INVALID | no |
| process not started | errors.INVALID | no |
| write failed | errors.INTERNAL | no |

**Example:**

```lua
proc:start()
local ok, err = proc:write_stdin("input data\n")
if err then error(err) end
```

#### process:stdout_stream() → Stream, error

Returns a Stream object for reading process stdout. Calling multiple times returns the same stream.

**Returns:**
- Success: `Stream, nil` - stream object with read/close methods
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| process closed | errors.INVALID | no |
| stdout not available | errors.INTERNAL | no |
| resource table unavailable | errors.INTERNAL | no |

**Example:**

```lua
local stdout, err = proc:stdout_stream()
if err then error(err) end

proc:start()
local data, rerr = stdout:read()
if rerr then error(rerr) end

stdout:close()
```

#### process:stderr_stream() → Stream, error

Returns a Stream object for reading process stderr. Calling multiple times returns the same stream.

**Returns:**
- Success: `Stream, nil` - stream object with read/close methods
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| process closed | errors.INVALID | no |
| stderr not available | errors.INTERNAL | no |
| resource table unavailable | errors.INTERNAL | no |

**Example:**

```lua
local stderr, err = proc:stderr_stream()
if err then error(err) end

proc:start()
proc:wait()
```

### ProcessExitChannel

Returned by `process:done()`. A standard channel carrying a single exit value.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| receive | () | ProcessExit, boolean | Yields until the child exits; `nil, false` once the value has been taken |
| case_receive | () | case | Case for `channel.select` |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local executor, err = exec.get("app:exec")
if err then
    if err:kind() == errors.INVALID then
        -- invalid input or permission denied
    elseif err:kind() == errors.INTERNAL then
        -- internal error (resource not found, etc.)
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`, `errors.UNAVAILABLE`

## Example

```lua
local exec = require("exec")

local executor, err = exec.get("app:exec")
if err then error(err) end

local proc, perr = executor:exec("echo hello", {
    work_dir = "/tmp",
    env = { MY_VAR = "value" }
})
if perr then error(perr) end

local stdout, serr = proc:stdout_stream()
if serr then error(serr) end

local ok, starterr = proc:start()
if starterr then error(starterr) end

local data, rerr = stdout:read()
if rerr then error(rerr) end
print("Output:", data)

local exitCode, werr = proc:wait()
if werr then error(werr) end
if exitCode ~= 0 then
    error("process failed with code " .. exitCode)
end

stdout:close()
executor:release()
```
