# Lua Payload Module Specification

## Overview

The `payload` module provides operations for creating and transcoding data between different formats (JSON, YAML, Lua, Go, etc.) with lazy transcoding support.

## Module Interface

### Module Loading

```lua
local payload = require("payload")
```

### Constants

#### payload.format

Table containing format constants:
- `payload.format.JSON`: JSON format
- `payload.format.YAML`: YAML format
- `payload.format.STRING`: Plain string format
- `payload.format.GOLANG`: Go native format
- `payload.format.LUA`: Lua native format
- `payload.format.BYTES`: Raw bytes format
- `payload.format.ERROR`: Error format

### Functions

#### payload.new(value: any)

Creates a new payload object from a Lua value.

Parameters:
- `value`: Any Lua value or error

Returns:
- `payload`: Payload object

Notes:
- Automatically detects errors and wraps them in ERROR format
- Non-error values are wrapped in LUA format

### Payload Methods

#### payload:get_format()

Returns the current format of the payload.

Returns:
- `format`: Format string (e.g., "lua/any", "json/plain")

#### payload:data()

Returns the raw data if the payload is in Lua format.

Returns:
- `data`: Lua value if format is LUA, nil otherwise

#### payload:transcode(target_format: string)

Transcodes the payload to a different format.

Parameters:
- `target_format`: Target format string (use payload.format constants)

Returns:
- `transcoded`: New payload in the target format
- `error`: Error message (or nil on success)

#### payload:unmarshal(target: table|userdata)

Unmarshals the payload data into a target table or userdata.

Parameters:
- `target`: Target table or userdata to unmarshal into

Returns:
- `success`: Boolean indicating success
- `error`: Error message (or nil on success)

## Example Usage

```lua
local payload = require("payload")

-- Create a payload from Lua data
local data = {name = "John", age = 30}
local p = payload.new(data)

print("Format:", p:get_format())  -- Output: lua/any

-- Transcode to JSON
local json_payload, err = p:transcode(payload.format.JSON)
if err then
  print("Transcode error:", err)
  return
end

print("JSON format:", json_payload:get_format())  -- Output: json/plain

-- Get raw data from Lua payload
local original = p:data()
print("Original name:", original.name)  -- Output: John

-- Unmarshal into existing table
local target = {}
local ok, err = p:unmarshal(target)
if ok then
  print("Unmarshaled age:", target.age)  -- Output: 30
end

-- Create payload from error
local function may_fail()
  error("Something went wrong")
end

local success, err = pcall(may_fail)
if not success then
  local err_payload = payload.new(err)
  print("Error format:", err_payload:get_format())  -- Output: golang/error
end
```

## Notes

- Payloads use lazy transcoding - conversion happens only when needed
- Transcoding between formats preserves data structure
- Error payloads are automatically detected and handled specially
- Unmarshal modifies the target in-place
- Format constants ensure type safety when transcoding
