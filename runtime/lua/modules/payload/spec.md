# Lua Payload Module Specification

## Overview

The `payload` module provides payload transcoding and format conversion. This module is preloaded by default and available globally without requiring `require()`.

## Module Interface

The module is available globally as `payload`.

```lua
local p = payload.new({key = "value"})
```

## Format Constants

### payload.format

A table of format constants:

- `payload.format.JSON` - `"json/plain"`
- `payload.format.YAML` - `"yaml/plain"`
- `payload.format.STRING` - `"text/plain"`
- `payload.format.BYTES` - `"application/octet-stream"`
- `payload.format.MSGPACK` - `"application/msgpack"`
- `payload.format.LUA` - `"lua/any"`
- `payload.format.GOLANG` - `"golang/any"`
- `payload.format.ERROR` - `"golang/error"`

## Functions

### payload.new(value)

Creates a new payload from a Lua value.

Parameters:

- `value`: Any Lua value (string, number, boolean, table, nil, or error).

Returns:

- `payload`: A payload object.

```lua
local p = payload.new({key = "value"})
local str_p = payload.new("hello")
local num_p = payload.new(42)
```

## Payload Methods

### p:get_format()

Returns the format of the payload.

Returns:

- `format`: String format identifier (one of the `payload.format` constants).

```lua
local p = payload.new({key = "value"})
local fmt = p:get_format()  -- "lua/any"
```

### p:data()

Returns the raw data from the payload. For Lua format payloads, returns the original value. For other formats, transcodes to Lua format first.

Returns:

- `value`: The Lua value.
- `error`: Structured error if transcoding fails (or nil on success).

```lua
local p = payload.new({name = "test"})
local data = p:data()
print(data.name)  -- "test"
```

### p:transcode(format)

Transcodes the payload to a different format.

Parameters:

- `format`: Target format (one of `payload.format` constants).

Returns:

- `payload`: New payload in the target format (or nil on error).
- `error`: Structured error (or nil on success).

```lua
local p = payload.new({key = "value"})
local json_p, err = p:transcode(payload.format.JSON)
if err then
    print(err:kind())  -- errors.INTERNAL
end
```

### p:unmarshal()

Unmarshals the payload to a Lua value. Same as `data()` for Lua format payloads.

Returns:

- `value`: The Lua value (or nil on error).
- `error`: Structured error (or nil on success).

```lua
local p = payload.new({a = 1, b = 2})
local data = p:unmarshal()
print(data.a)  -- 1
```

### tostring(p)

Returns a string representation of the payload.

```lua
local p = payload.new("hello")
print(tostring(p))  -- "payload{format=lua/any}"
```

## Error Handling

The module returns structured errors using the standard error type.

### Error Types

1. **Internal Error:** Transcoding or unmarshaling failure.

```lua
local result, err = p:transcode(payload.format.JSON)
if err then
    -- err:kind() == errors.INTERNAL
    -- err:retryable() == false
end
```

## Thread Safety

- The `payload` module is thread-safe.
- Module table is immutable and shared across Lua states.
- Type metatable is registered once globally via `value.RegisterTypeMethods`.

## Module Classification

- **Class**: `encoding`, `deterministic`
- Operations are pure functions with no side effects.
- Same input always produces the same output.

## Example Usage

```lua
-- Create payload from table
local p = payload.new({
    name = "test",
    values = {1, 2, 3}
})

-- Check format
assert(p:get_format() == payload.format.LUA)

-- Get data back
local data = p:data()
print(data.name)  -- "test"

-- Create payload from different types
local str_payload = payload.new("hello world")
local num_payload = payload.new(123.456)
local bool_payload = payload.new(true)

-- Transcode to JSON (requires context with transcoder)
local json_p, err = p:transcode(payload.format.JSON)
if not err then
    print(json_p:get_format())  -- "json/plain"
end
```

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "payload",
    Description: "Payload transcoding and format conversion",
    Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
    Build:       buildModule,
}

func init() {
    value.RegisterTypeMethods(nil, typeName,
        map[string]lua.LGFunction{"__tostring": payloadToString},
        payloadMethods)
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
    mod := lua.CreateTable(0, 2)
    mod.RawSetString("new", lua.LGoFunc(newPayload))

    formats := lua.CreateTable(0, 8)
    formats.RawSetString("JSON", lua.LString(payload.JSON))
    // ... more formats
    mod.RawSetString("format", formats)

    mod.Immutable = true
    return mod, nil
}
```
