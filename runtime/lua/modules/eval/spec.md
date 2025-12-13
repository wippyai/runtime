# eval

Dynamic Lua code compilation and execution with manual process control. Process, nondeterministic.

## Loading

```lua
local eval = require("eval")
```

## Functions

### compile(source: string, method?: string, options?: table) → Program, error

Compiles Lua source code into a reusable Program.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| source | string | yes | - | Lua source code to compile |
| method | string | no | "" | Method name to call on execution |
| options | table | no | nil | Compilation options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| modules | string[] | nil | Module names allowed in sandboxed code |

**Returns:**
- Success: `Program` - compiled program userdata
- Error: `nil, error` - error is string

**Errors (strings):**
- Compilation failed - syntax errors or invalid Lua code
- Invalid program type - internal error during program creation

**Yields:** until compilation completes

### run(config: table) → result, error

Compiles and executes Lua code in one operation.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| config | table | yes | - | Execution configuration |

**config fields:**

| Field | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| source | string | yes | - | Lua source code to execute |
| method | string | no | "" | Method name to call |
| modules | string[] | no | nil | Module names allowed in code |
| args | any[] | no | nil | Arguments passed to method |
| context | {[string]: any} | no | nil | Context values available to code |

**Returns:**
- Success: `result` - return value from executed code (type depends on code)
- Error: `nil, error` - error is string

**Errors (strings):**
- Compilation or execution failed - syntax errors, runtime errors

**Yields:** until compilation and execution complete

### sandbox(source_or_id: string, options?: table) → Sandbox

Creates a Sandbox for manual step-by-step process control.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| source_or_id | string | yes | - | Lua source code or registry ID (format: "type:path") |
| options | table | no | nil | Sandbox options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| modules | string[] | nil | Module names allowed in sandboxed code |

**Returns:** `Sandbox` - sandbox userdata for manual stepping

**Notes:**
- Distinguishes registry IDs from source by checking for ":" and length (<256 chars)
- Source code typically contains newlines; registry IDs do not

## Types

### Program

Returned by `eval.compile()`. Represents compiled Lua code.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| method | () | string | Method name configured at compile time |
| modules | () | string[] | Allowed module names |

### Sandbox

Returned by `eval.sandbox()`. Provides manual control over child process execution.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| execute | (method: string, ...args) | ok: boolean, error: string \| nil | Starts execution, can only be called once |
| step | (results?: table) | step_result: table | Advances process one step |
| close | () | - | Closes sandbox and frees resources |

#### sandbox:execute(method: string, ...args) → ok: boolean, error: string | nil

Initializes and starts sandbox execution.

**Parameters:**
- `method` - method name to invoke on loaded code
- `...args` - variable arguments passed to method

**Returns:**
- `true` on success
- `false, error` if execution fails to start

**Errors (strings):**
- "sandbox already started" - execute called multiple times
- "eval host not available" - no eval host in context
- "compile error: ..." - source compilation failed
- "failed to create process: ..." - process creation failed
- "execute error: ..." - execution initialization failed

#### sandbox:step(results?: table) → step_result: table

Advances sandbox execution by one step.

**Parameters:**
- `results` - optional table with results from previous yields

**results fields:**

| Field | Type | Notes |
|-------|------|-------|
| data | any | Result data from resolved yield |
| error | string | Error message if yield failed |

**Returns:** table with step result

**step_result fields:**

| Field | Type | Present When | Notes |
|-------|------|--------------|-------|
| status | string | always | Step status: "done", "idle", "continue", "waiting", "error" |
| value | any | status = "done" | Final return value from code |
| error | string | status = "error" | Error message |
| yields | table[] | status = "continue" | Array of yield commands to be resolved |

**yields[i] fields (yield commands):**

Each yield is a table with fields depending on command type. Common fields:

| Field | Type | Notes |
|-------|------|-------|
| id | integer | Command ID |
| type | string | Command type name |

**Clock yield types:**

| Type | Additional Fields |
|------|-------------------|
| sleep | duration: integer (nanoseconds) |
| ticker_start | duration: integer (nanoseconds) |
| ticker_stop | ticker_id: integer |
| timer_start | duration: integer (nanoseconds) |
| timer_wait | timer_id: integer |
| timer_stop | timer_id: integer |
| timer_reset | timer_id: integer, duration: integer |
| unknown | - (for unregistered command types) |

**Status values:**
- `"done"` - execution completed, see `value` field
- `"idle"` - process idle, no work to do
- `"continue"` - process has yields to resolve, see `yields` field
- `"waiting"` - process waiting for external event
- `"error"` - execution failed, see `error` field

**Errors (strings):**
- "sandbox not started, call execute first" - step called before execute
- Step execution errors - internal process errors

#### sandbox:close()

Closes the sandbox and releases all resources.

**Notes:**
- Safe to call multiple times
- Automatically called on parent process exit via resource cleanup
- Should be called explicitly when done to free resources earlier

## Dependencies

### evalhost.Program

Internal program representation created by compilation. Not directly accessible from Lua, wrapped by Program type.

### process.Process

Internal process handle for sandbox stepping. Not directly accessible from Lua, managed by Sandbox type.

## Errors

This module returns string errors, not structured errors.

## Example

```lua
local eval = require("eval")

-- Example 1: Quick execution with run()
local result, err = eval.run({
    source = [[
        local function handle(x, y)
            return x + y
        end
        return { handle = handle }
    ]],
    method = "handle",
    args = { 10, 20 },
    modules = { "json" }
})
if err then error(err) end
print(result)  -- 30

-- Example 2: Compile once, reuse Program
local program, err = eval.compile([[
    local function calc(n)
        return n * 2
    end
    return { calc = calc }
]], "calc", { modules = {} })
if err then error(err) end

print(program:method())    -- "calc"
print(program:modules())   -- empty table

-- Example 3: Manual process control with sandbox
local sb = eval.sandbox([[
    local time = require("time")
    local function handle()
        time.sleep(100 * time.MILLISECOND)
        return "done"
    end
    return { handle = handle }
]], { modules = { "time" } })

local ok, err = sb:execute("handle")
if not ok then error(err) end

-- Step through yields
while true do
    local result = sb:step()

    if result.status == "done" then
        print("Result:", result.value)
        break
    end

    if result.status == "error" then
        error(result.error)
    end

    if result.status == "continue" then
        -- Inspect yields
        for _, yield in ipairs(result.yields) do
            print("Yield type:", yield.type)
            if yield.type == "sleep" then
                print("  Duration:", yield.duration)
            end
        end

        -- Provide mock result (in real scheduler, dispatcher handles this)
        sb:step({ data = 1000000000000000 })
    end
end

sb:close()
```
