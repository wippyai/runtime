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
| process_group | boolean | executor default | Start the child in its own process group so `signal()` and `close()` reach its descendants |

**PTYOptions fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| width | integer | 80 | Initial PTY columns |
| height | integer | 24 | Initial PTY rows |
| term | string | nil | Child `TERM` value, such as `xterm-256color` |

**Process groups**

By default the child shares the runtime's process group, so a signal reaches
only the child itself; anything it spawns survives and is reparented. With
`process_group = true` the child leads a group of its own, and `signal()`,
`close()` and the cleanup that runs when the owning process exits address the
whole group. Use it for a command that spawns its own subprocesses, such as a
tool harness or a language server.

The executor entry sets the default with its `process_group` config field; the
per-command option overrides it in both directions. A PTY-backed child already
has a session of its own, so the option only changes how signals are addressed
there. Process groups are a Unix facility: on Windows the option is rejected
with `errors.UNAVAILABLE` rather than silently ignored.

A container executor confines the tree to the container's pid namespace, where
signaling the container's first process already ends everything it spawned, so
the option carries no additional meaning there.

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
| process_group requested on Windows | errors.UNAVAILABLE | no |
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
| wait | () | integer, error | Waits for process to exit, yields |
| signal | (sig: integer) | boolean, error | Sends signal to process, or to its group when `process_group` is set |
| pid | () | integer, error | Host process id of the started child |
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

The session exposes `send(event)`, `done()`, `status()`, and `close()`.
Resize, keyboard, mouse, focus, and paste events use the canonical `tty`
event records.

#### process:resize(width: integer, height: integer) → boolean, error

Resizes an allocated PTY before it is transferred to a terminal session.
Ordinary pipe-backed processes return an error.

#### process:close(force?: boolean) → boolean, error

Releases the process. A started child is sent `SIGTERM`, or `SIGKILL` when
`force` is true, then reaped; an unstarted handle is simply invalidated. A
process started with `process_group` is signaled as a group, so its descendants
go with it.

Reaping is what releases the child's entry in the OS process table; without it a
stopped process lingers as a zombie for the lifetime of the runtime. It happens
in the background so `close()` does not block, and a child still running after a
grace period is killed so the reap always completes.

Because reaping closes the process's stdout and stderr pipes, its streams are
finished with once it is closed. Read any output you need before calling this.

After `close()` every method on the process, including `wait()`, reports
`process closed`. Use `signal()` and `wait()` instead when the exit code matters.

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

Waits for the process to exit and returns the exit code. Automatically closes the process.

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

#### process:signal(sig: integer) → boolean, error

Sends a signal to the running process, or to its whole process group when the
process was created with `process_group = true`.

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

#### process:pid() → integer, error

Returns the operating system process id of the started child, for recording
which process an attempt ran as.

**Returns:**
- Success: `pid: integer, nil`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| process closed | errors.INVALID | no |
| process not started | errors.INVALID | no |
| executor exposes no host pid | errors.UNAVAILABLE | no |

**Example:**

```lua
proc:start()
local pid, err = proc:pid()
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
