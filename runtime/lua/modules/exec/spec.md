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
| release | () | boolean, error | Releases the executor resource |

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
| process_group | boolean | executor default | Start the child in its own process group so signals reach descendants; unsupported on Windows |

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
| attach_terminal | () | TerminalSession, error | Attaches an unstarted PTY process to the current TTY |
| close | (force?: boolean) | boolean, error | Signals the process, reaps it, releases the handle |

#### process:attach_terminal() → TerminalSession, error

Consumes an unstarted PTY-backed process and attaches it to the current
process terminal. The returned session becomes the exclusive lifecycle owner;
the original process handle cannot be used afterward.

```lua
local child = assert(executor:exec("bash", {
    pty = {width = 80, height = 24, term = "xterm-256color"},
}))
local session = assert(child:attach_terminal())
```

The session exposes `send(event)`, `done()`, `pid()`, `status()`, and `close()`.
`pid()` returns the host process identifier when the attached process exposes
that capability. The call may return a not-started error while the asynchronous
attachment is starting; once the session has completed with a startup error,
it returns that terminal error instead of a retryable pending result.
completion channel's `receive()` and `case_receive()` values are booleans, so a
`channel.select` result from `done():case_receive()` carries a boolean value.
Resize, keyboard, mouse, focus, and paste events use the canonical `tty`
event records.

#### process:resize(width: integer, height: integer) → boolean, error

Resizes an allocated PTY before it is transferred to a terminal session.
Ordinary pipe-backed processes return an error.

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
