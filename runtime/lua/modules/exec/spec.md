# Lua Executor Module Specification

## Overview

The executor module provides a Lua interface for executing tasks synchronously and asynchronously with context
management capabilities. It enables Lua code to interact with an underlying execution system while maintaining proper
context and error handling.

## Module Interface

### Module Loading

```lua
local executor = require("executor")
```

### Global Functions

#### executor.new()

Creates a new executor instance.

Returns:

- `userdata`: A new executor instance with an empty context

#### executor.call(target: string, ...args)

Executes a task synchronously with the given target name and optional arguments.

Parameters:

- `target`: String identifier for the target function/task
- `...args`: Variable number of arguments to pass to the target

Returns:

- `result`: The execution result (or nil on error)
- `error`: Error message string (or nil on success)

#### executor.run(target: string, ...args)

Executes a task asynchronously with the given target name and optional arguments.

Parameters:

- `target`: String identifier for the target function/task
- `...args`: Variable number of arguments to pass to the target

Returns:

- `error`: Error message string (or nil on success)

### Instance Methods

#### executor:with_context(context_table)

Sets the context values for the executor instance.

Parameters:

- `context_table`: Table with string keys and values to be used as context

Returns:

- `userdata`: The executor instance (allows method chaining)

Example:

```lua
local exec = executor.new()
exec = exec:with_context({
    user = "john",
    role = "admin"
})
```

#### executor:call(target: string, ...args)

Instance method version of the global call function. Executes a task synchronously using the instance's context.

Parameters and returns are the same as the global `call` function.

#### executor:run(target: string, ...args)

Instance method version of the global run function. Executes a task asynchronously using the instance's context.

Parameters and returns are the same as the global `run` function.

## Error Handling

The module returns errors in the following cases:

1. Missing target name (empty string)

```lua
local result, err = executor.call("")  -- err: "target name is required"
```

2. Context-related errors:
    - No context found
    - Executor not found in context
    - Transcoder not found in context

3. Invalid context keys:

```lua
exec:with_context({[1] = "value"})  -- Error: "context keys must be strings"
```

4. Execution cancellation:

```lua
local result, err = executor.call("test_function")
-- If cancelled: err = "execution cancelled"
```

## Context Management

1. Each executor instance maintains its own independent context
2. Context values persist until explicitly changed
3. Creating a new executor instance starts with an empty context
4. Context updates create new state without affecting the original instance

Example of context isolation:

```lua
local exec1 = executor.new():with_context({user = "user1"})
local exec2 = executor.new():with_context({user = "user2"})

-- Each maintains its own context
local result1, err1 = exec1:call("test_function")
local result2, err2 = exec2:call("test_function")
```

## Payload Handling

1. Arguments passed to `call` or `run` are converted to payloads
2. Nil arguments are skipped in payload creation
3. Results are transcoded back to Lua values
4. Nil payloads in results are returned as nil values

## Thread Safety

1. The executor module is designed to be thread-safe
2. Each instance maintains isolated context
3. Context values are copied rather than shared
4. Async operations (`run`) don't block the calling thread

## Best Practices

1. Always check for errors in the return values
2. Use instance methods with `with_context` for related operations
3. Use global methods for one-off executions
4. Prefer `call` for operations requiring return values
5. Use `run` for fire-and-forget operations
6. Keep context keys as strings
7. Chain context updates when needed:

```lua
local exec = executor.new()
    :with_context({user = "john"})
    :with_context({role = "admin"})
```