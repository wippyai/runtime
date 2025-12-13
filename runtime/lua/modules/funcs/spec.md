# funcs

Function calls and async execution. Workflow, nondeterministic.

## Loading

```lua
local funcs = require("funcs")
```

## Functions

### call(target: string, ...args) -> result, error

Calls a registered function synchronously.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| target | string | yes | - | Function ID in format "namespace:name" |
| ...args | any | no | - | Arguments passed to the function |

**Returns:**
- Success: `result, nil` - the function's return value
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| target empty | errors.INVALID | no |
| namespace missing | errors.INVALID | no |
| name missing | errors.INVALID | no |
| permission denied | errors.PERMISSION_DENIED | no |
| function error | varies | varies |

**Yields:** until function completes

**Context inheritance:** Automatically inherits actor, scope, and values from calling frame context.

```lua
local result, err = funcs.call("app.test:echo", "hello")
if err then error(err) end
print(result.echo)  -- "hello"
```

### async(target: string, ...args) -> Future, error

Starts an async function call, returns immediately.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| target | string | yes | - | Function ID in format "namespace:name" |
| ...args | any | no | - | Arguments passed to the function |

**Returns:**
- Success: `Future, nil` - Future object for awaiting result
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| target empty | errors.INVALID | no |
| namespace missing | errors.INVALID | no |
| name missing | errors.INVALID | no |
| permission denied | errors.PERMISSION_DENIED | no |
| subscribe failed | errors.INTERNAL | no |

**Yields:** until async operation starts (not until completion)

**Context inheritance:** Automatically inherits actor, scope, and values from calling frame context.

```lua
local future, err = funcs.async("app.process:heavy", 42)
if err then error(err) end

local ch = future:response()
local payload, ok = ch:receive()
local data = payload:data()
```

### new() -> Executor

Creates a new Executor for building function calls with custom context.

**Returns:** `Executor` - new executor instance

```lua
local exec = funcs.new()
local result = exec:call("app.test:echo", "test")
```

## Types

### Executor

Builder for function calls with custom context options. Methods return new Executor instances (immutable chaining).

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| with_context | (values: table) | Executor | Merges context values |
| with_actor | (actor: Actor) | Executor | Sets security actor |
| with_scope | (scope: Scope) | Executor | Sets security scope |
| with_options | (options: table) | Executor | Sets call options |
| call | (target: string, ...args) | result, error | Sync call |
| async | (target: string, ...args) | Future, error | Async call |

#### executor:with_context(values: table) -> Executor

Adds or merges context values for called functions.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| values | table | yes | - | Key-value pairs to add to context |

**Returns:** New `Executor` with merged context

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| permission denied | errors.PERMISSION_DENIED | no |

**Context merging:** Values from calling frame are inherited, then overlaid with explicit values. Explicit values take precedence for conflicts.

```lua
local exec = funcs.new():with_context({
    request_id = "req-123",
    user_id = 456
})
local result = exec:call("app.api:handler")
```

#### executor:with_actor(actor: Actor) -> Executor

Sets the security actor for called functions.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| actor | Actor | yes | - | Security actor (from security module) |

**Returns:** New `Executor` with actor set

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| actor is nil | error | no |
| permission denied | errors.PERMISSION_DENIED | no |

```lua
local security = require("security")
local actor = security.new_actor("user123", {role = "admin"})

local exec = funcs.new():with_actor(actor)
local result = exec:call("app.secure:operation")
```

#### executor:with_scope(scope: Scope) -> Executor

Sets the security scope for called functions.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| scope | Scope | yes | - | Security scope (from security module) |

**Returns:** New `Executor` with scope set

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| scope is nil | error | no |
| permission denied | errors.PERMISSION_DENIED | no |

```lua
local security = require("security")
local scope = security.new_scope()

local exec = funcs.new():with_scope(scope)
local result = exec:call("app.secure:operation")
```

#### executor:with_options(options: table) -> Executor

Sets call options (timeout, retries, etc).

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | yes | - | Implementation-specific options |

**Returns:** New `Executor` with options set

Options are passed as Task.Options to the scheduler. Available options depend on the runtime configuration.

```lua
local exec = funcs.new():with_options({
    timeout = 5000,
    priority = "high"
})
```

#### executor:call(target: string, ...args) -> result, error

Calls a function with the executor's context settings.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| target | string | yes | - | Function ID in format "namespace:name" |
| ...args | any | no | - | Arguments passed to the function |

**Returns:**
- Success: `result, nil` - the function's return value
- Error: `nil, error` - structured error

**Errors:** Same as `funcs.call()`

**Yields:** until function completes

**Context application:** Starts with inherited frame context (actor, scope, values), then overlays with executor's explicit settings. Explicit settings take precedence.

```lua
local result, err = funcs.new()
    :with_context({trace_id = "abc"})
    :call("app.test:echo", "hello")
```

#### executor:async(target: string, ...args) -> Future, error

Starts an async call with the executor's context settings.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| target | string | yes | - | Function ID in format "namespace:name" |
| ...args | any | no | - | Arguments passed to the function |

**Returns:**
- Success: `Future, nil` - Future object for awaiting result
- Error: `nil, error` - structured error

**Errors:** Same as `funcs.async()`

**Yields:** until async operation starts

**Context application:** Same as `executor:call()`

```lua
local future, err = funcs.new()
    :with_context({session_id = "sess-123"})
    :async("app.process:heavy", 42)
```

## Dependencies

### Future (from future module)

Returned by `funcs.async()` and `executor:async()`.

| Method | Signature | Returns |
|--------|-----------|---------|
| response | () | Channel |
| channel | () | Channel |
| is_complete | () | boolean |
| is_canceled | () | boolean |
| result | () | value, error |
| error | () | error, boolean |
| cancel | () | - |

See: `runtime/lua/modules/future/spec.md`

### Channel (from engine)

Used by Future for receiving async results.

| Method | Signature | Returns |
|--------|-----------|---------|
| receive | () | value, ok: boolean |
| close | () | - |
| case_receive | () | case |

See: `runtime/lua/engine/spec.md`

### Payload (from payload module)

Async results are returned as Payload userdata.

| Method | Signature | Returns |
|--------|-----------|---------|
| data | () | value, error |
| get_format | () | string |

See: `runtime/lua/modules/payload/spec.md`

### Actor and Scope (from security module)

Used by `with_actor()` and `with_scope()`.

See: `runtime/lua/modules/security/spec.md`

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = funcs.call("app.test:echo", "hello")
if err then
    if err:kind() == errors.INVALID then
        -- bad input (empty target, missing namespace/name)
    elseif err:kind() == errors.PERMISSION_DENIED then
        -- not allowed to call this function
    elseif err:kind() == errors.INTERNAL then
        -- function execution failed
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.PERMISSION_DENIED`, `errors.INTERNAL`, `errors.CANCELED`, or any error kind from the called function

## Example

```lua
local funcs = require("funcs")

-- Simple synchronous call
local result, err = funcs.call("app.test:echo", "hello world")
if err then error(err) end
print(result.echo)  -- "hello world"

-- Async call with Future
local future, err = funcs.async("app.process:heavy_compute", 42)
if err then error(err) end

-- Wait for result via channel
local ch = future:response()
local payload, ok = ch:receive()
if not ok then error("channel closed") end
local data = payload:data()
print("async result:", data)

-- Using executor with context
local exec = funcs.new():with_context({
    request_id = "req-456",
    user_id = 123
})

local r1, e1 = exec:call("app.api:get_user")
if e1 then error(e1) end

-- Chaining multiple context settings
local security = require("security")
local actor = security.new_actor("admin", {role = "admin"})

local result2 = funcs.new()
    :with_actor(actor)
    :with_context({operation = "delete"})
    :with_options({timeout = 5000})
    :call("app.admin:delete_user", 999)

-- Multiple concurrent async calls
local f1 = funcs.async("app.process:task_a")
local f2 = funcs.async("app.process:task_b")
local f3 = funcs.async("app.process:task_c")

-- Wait for all
local p1 = f1:response():receive()
local p2 = f2:response():receive()
local p3 = f3:response():receive()

print("all tasks complete")
```
