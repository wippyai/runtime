<!-- SPDX-License-Identifier: MPL-2.0 -->

# eval_runner

Execute untrusted Lua code via dispatcher with security checks. Process, nondeterministic.

## Loading

```lua
local runner = require("eval_runner")
```

## Permissions

This module requires security permissions for all operations:

| Action | Resource | Description |
|--------|----------|-------------|
| eval.compile | "" | Permission to compile Lua source code |
| eval.run | "" | Permission to run Lua code |

## Functions

### compile(source: string, method?: string, options?: table) -> Program, error

Compiles Lua source code into a reusable Program.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| source | string | yes | - | Lua source code to compile |
| method | string | no | "" | Method name to call on execution |
| options | table | no | nil | Compilation options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| modules | string[] | nil | Module names allowed in executed code |

**Returns:**
- Success: `Program, nil` - compiled program userdata
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |
| permission denied | errors.PERMISSION_DENIED | no |
| compilation failed | errors.INTERNAL | no |

**Yields:** until compilation completes

**Example:**

```lua
local program, err = runner.compile([[
    local function handle(x)
        return x * 2
    end
    return { handle = handle }
]], "handle", { modules = { "json" } })
if err then error(err) end

print(program:method())  -- "handle"
```

### run(config: table) -> result, error

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
- Success: `result, nil` - return value from executed code (type depends on code)
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |
| permission denied | errors.PERMISSION_DENIED | no |
| source is required | errors.INVALID | no |
| compilation failed | errors.INTERNAL | no |
| execution failed | errors.INTERNAL | no |

**Yields:** until compilation and execution complete

**Example:**

```lua
local result, err = runner.run({
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
```

## Types

### Program

Returned by `runner.compile()`. Represents compiled Lua code.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| method | () | string | Method name configured at compile time |
| modules | () | string[] | Allowed module names |

## Module Classes

- `process` - Requires process-level access
- `nondeterministic` - Results may vary between executions

## Dependencies

### evalhost.Program

Internal program representation created by compilation. Not directly accessible from Lua, wrapped by Program type.

## Errors

This module returns structured errors with `:kind()` and `:message()` methods.
