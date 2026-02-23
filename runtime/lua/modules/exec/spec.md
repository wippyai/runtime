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
| exec | (cmd: string, options?: table) | Process, error | Creates new process with command |
| release | () | boolean, error | Releases the executor resource |

#### executor:exec(cmd: string, options?: table) → Process, error

Creates a new process with the specified command.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| cmd | string | yes | - | Command to execute |
| options | table | no | nil | Process options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| work_dir | string | nil | Working directory for the process |
| env | {[string]: string} | nil | Environment variables as key-value map |

**Returns:**
- Success: `Process, nil` - process object (not started)
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| executor released | errors.INVALID | no |
| cmd is empty string | errors.INVALID | no |
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
| wait | () | integer, error | Waits for process to exit, yields |
| signal | (sig: integer) | boolean, error | Sends signal to process |
| write_stdin | (data: string) | boolean, error | Writes to process stdin |
| stdout_stream | () | Stream, error | Returns stdout stream |
| stderr_stream | () | Stream, error | Returns stderr stream |
| close | (force?: boolean) | boolean, error | Closes process with signal |

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

#### process:close(force?: boolean) → boolean, error

Closes the process by sending a signal. Safe to call multiple times.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| force | boolean | no | false | If true, sends SIGKILL (9); if false, sends SIGTERM (15) |

**Returns:** `true, nil` - always succeeds

**Example:**

```lua
-- Graceful shutdown
proc:close()

-- Force kill
proc:close(true)
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

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

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
