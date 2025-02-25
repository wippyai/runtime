# Lua Functions Module Specification

## Overview

The Functions module provides a Lua interface for executing tasks both synchronously and asynchronously. It enables
namespace-aware function calls with proper context management and method chaining.

## Module Interface

### Module Loading

```lua
local funcs = require("funcs")
```

### Creating Function Executor

```lua
local executor = funcs.new()
```

Returns a new function executor instance or raises an error if dependencies are missing.

### Methods

#### with_context(context_table)

Creates a new executor instance with merged context values.

```lua
-- Create new executor with context
local executor2 = executor:with_context({
    tenant_id = "123",
    user_id = "456"
})

-- Original executor remains unchanged
-- Chain multiple contexts
local executor3 = funcs.new()
    :with_context({ tenant = "123" })
    :with_context({ user = "john" })
```

Parameters:

- `context_table`: Table with string keys and any values

Returns:

- New executor instance with updated context (immutable operation)

#### call(target, ...)

Executes function synchronously.

```lua
-- Call with single values
local result, err = executor:call("myapp:process", 1, 2, 3)

-- Call with table
local result, err = executor:call("myapp:process", {
    user_id = 123,
    data = "some data"
})
```

Parameters:

- `target`: String in "namespace:name" format (namespace required)
- `...`: Arguments to pass (auto-wrapped in payloads)

Returns:

- `result`: Function result or nil on error
- `err`: Error message or nil on success

#### run(target, ...)

Executes function asynchronously.

```lua
-- Run with single values
local err = executor:run("myapp:worker", 1, 2, 3)

-- Run with table
local err = executor:run("myapp:worker", {
    job_id = "job123",
    data = "async data"
})
```

Parameters:

- `target`: String in "namespace:name" format (namespace required)
- `...`: Arguments to pass (auto-wrapped in payloads)

Returns:

- `err`: Error message or nil on success

## Usage Examples

### Basic Usage

```lua
local funcs = require("funcs")

-- Create executor
local executor = funcs.new()

-- Synchronous call
local result, err = executor:call("myapp:process", 1, 2, 3)

-- Async run
local err = executor:run("myapp:worker", { job_id = "123" })
```

### Context and Chaining

```lua
local funcs = require("funcs")

-- Chain operations with context
local result, err = funcs.new()
    :with_context({ tenant = "123" })
    :with_context({ user = "john" })
    :call("myapp:process", 1, 2, 3)

-- Context is immutable
local base = funcs.new():with_context({ tenant = "123" })
local exec1 = base:with_context({ user = "john" })
local exec2 = base:with_context({ user = "jane" })

-- base only has tenant
-- exec1 has tenant and user="john"
-- exec2 has tenant and user="jane"
```

### Argument Handling

```lua
-- Single values are auto-wrapped
executor:call("myapp:process", 1, "string", true)

-- Tables are preserved
executor:call("myapp:process", {
    id = 1,
    nested = {
        key = "value"
    }
})
```

## Error Handling

```lua
-- Handle call errors
local result, err = executor:call("myapp:process", data)
if err then
    print("Error:", err)
    return
end

-- Handle run errors
local err = executor:run("myapp:worker", data)
if err then
    print("Error:", err)
    return
end
```

## Important Notes

1. Namespace is required in target function names (`namespace:name`)
2. Context is immutable - each `with_context` creates a new executor
3. All methods support chaining
4. Single value arguments are automatically wrapped
5. Table arguments are preserved as-is
6. Context keys must be strings