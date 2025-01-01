# Lua Context Management Module Specification

## Overview

The `ctx` module provides a Lua interface for interacting with a context management system. It allows Lua code to get
and set values within a shared context, enabling communication and data sharing between different components of a Lua
application or between Lua and an external system.

## Module Interface

### Module Loading

```lua
local ctx = require("ctx")
```

### Global Functions

#### ctx.get(key: string)

Retrieves a value from the context associated with the given key.

Parameters:

- `key`: String identifier for the value to retrieve

Returns:

- `value`: The value associated with the key (or nil if not found or an error occurs)
- `error`: Error message string (or nil on success)

#### ctx.set(key: string, value: any)

Sets a value in the context for the given key.

Parameters:

- `key`: String identifier for the value to set
- `value`: The value to associate with the key

Returns:

- `ok`: Boolean indicating success (true) or failure (false)
- `error`: Error message string (or nil on success)

## Error Handling

The module returns errors in the following cases:

1. **Missing Context:** If no context is found or the context is invalid.

    ```lua
    local val, err = ctx.get("someKey")  -- err: "no context found" or "invalid context"
    ```

2. **Empty Key:** If an empty key is provided.

    ```lua
    local val, err = ctx.get("")  -- err: "empty key provided"
    ```

3. **Key Not Found:** If the key does not exist in the context (only for `ctx.get`).

    ```lua
    local val, err = ctx.get("nonExistentKey")  -- val: nil, err: "no value found for key: nonExistentKey"
    ```

4. **Invalid Context Type:**  If a value of the wrong type is used as the context store.

    ```lua
    local val, err = ctx.get("someKey") -- err: "invalid context"
    ```

## Context Management

1. The context is assumed to be managed by an underlying system accessible to the Lua state.
2. This system must provide a way to store and retrieve values associated with string keys.

## Payload Handling

1. Values passed to `ctx.set` may need conversion from Lua types to types understood by the underlying context system.
2. Values returned by `ctx.get` may need conversion from types used by the context system to Lua types.
3. Conversion errors during `ctx.set` are handled internally, and the corresponding error message is logged.
4. Nil values are handled appropriately during any conversions.

## Thread Safety

1. Thread safety is primarily managed by the underlying context implementation.
2. The `ctx` module itself does not introduce any shared mutable state.
3. It is assumed that the underlying context implementation is thread-safe if used in a multi-threaded environment.

## Best Practices

1. **Always check for errors:** Always check the `error` return value from `ctx.get` and `ctx.set`.
2. **Use appropriate context:** Ensure that a valid context is available to the Lua state before using the `ctx` module.
3. **Handle conversion errors:** Be aware of potential type conversion issues when setting values, especially when
   passing complex Lua data structures to the context.
4. **Use meaningful keys:** Choose descriptive string keys for context values to improve code readability and
   maintainability.
5. **Validate context type:** If possible, verify that the underlying context is of the expected type before using the
   `ctx` module.
6. **Keep context keys as strings.**

## Example Usage

```lua
local ctx = require("ctx")

-- Set a value in the context
local ok, err = ctx.set("myKey", "myValue")
if not ok then
  print("Error setting value:", err)
end

-- Get a value from the context
local value, err = ctx.get("myKey")
if err then
  print("Error getting value:", err)
else
  print("Value:", value)
end

-- Example with a table
local myTable = { name = "John", age = 30 }
local ok, err = ctx.set("user", myTable)
if not ok then
    print("Error setting table:", err)
end

local retrievedTable, err = ctx.get("user")
if err then
    print("Error getting table:", err)
elseif retrievedTable then
    print("User name:", retrievedTable.name)
    print("User age:", retrievedTable.age)
end
```
