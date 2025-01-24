# Lua Upstream Module Specification

## Overview

The `upstream` module provides functionality to send values from Lua code to the parent runtime. It implements a
non-blocking channel-based communication mechanism that allows Lua scripts to pass data upstream to the host
application. Values not automatically repacked.

## Module Interface

### Module Loading

```lua
local upstream = require("upstream")
```

> Module is typically expected to be automatically preloaded, direct use of `require` is not necessary.

### Global Functions

#### upstream.send(value: any)

Sends a value upstream to the parent runtime environment.

Parameters:

- `value`: Any Lua value to be sent upstream (nil, boolean, number, string, table)

Returns:

- `success`: A boolean indicating whether the value was successfully sent
    - `true`: The value was successfully sent upstream
    - `false`: The send operation would block (channel is full)

## Behavior

### Value Handling

1. **Types:**
    - Values passed as int, no conversion is performed

2. **Non-blocking Operation:**
    - The send operation never blocks
    - If the upstream channel is full, the function returns false immediately
    - A return value of false indicates the value was not sent

### Thread Safety

- The module is designed to be thread-safe
- Multiple Lua states can safely send values concurrently
- The order of values received upstream is not guaranteed when multiple scripts are sending simultaneously

## Example Usage

```lua
-- Send a simple value
local success = upstream.send("hello")
if not success then
    print("Failed to send string")
end

-- Send a number
success = upstream.send(42.5)
if not success then
    print("Failed to send number")
end

-- Example with error handling
local function process_and_send(data)
    -- Process data
    local result = some_processing(data)
    
    -- Try to send result upstream
    if not upstream.send(result) then
        -- Handle send failure
        log_error("Failed to send result upstream")
        return false
    end
    return true
end
```