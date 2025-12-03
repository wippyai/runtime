# Lua Base64 Module Specification

## Overview

The `base64` module provides Base64 encoding and decoding functions for string data. It uses standard Base64 encoding (RFC 4648) with padding.

## Module Interface

### Module Loading

```lua
local base64 = require("base64")
```

### Functions

#### base64.encode(data: string)

Encodes a string to Base64 format.

Parameters:

- `data`: String to encode.

Returns:

- `encoded`: Base64 encoded string (or nil on error).
- `error`: Structured error object (or nil on success).

#### base64.decode(data: string)

Decodes a Base64 encoded string.

Parameters:

- `data`: Base64 encoded string to decode.

Returns:

- `decoded`: Decoded string (or nil on error).
- `error`: Structured error object (or nil on success).

## Error Handling

The module returns structured errors using the `lua.Error` type. See [errors.md](../errors.md) for full error specification.

### Error Types

1. **Invalid Input Type:** If input is not a string.

```lua
local result, err = base64.encode(123)
-- result: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
-- tostring(err) == "string expected"
```

2. **Invalid Base64 Data:** If input contains invalid Base64 characters.

```lua
local result, err = base64.decode("!!!invalid!!!")
-- result: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
-- tostring(err) contains "illegal base64"
```

### Error Kind Comparison

Always use `errors.*` constants for kind comparison:

```lua
local result, err = base64.decode(input)
if err then
    if err:kind() == errors.INVALID then
        -- handle invalid input
    end
end
```

### Error Concatenation

Errors support string concatenation for logging:

```lua
local result, err = base64.decode("invalid")
if err then
    print("error: " .. err)  -- Works without explicit tostring()
end
```

## Behavior

1. **Empty String Handling**
   - Both `encode` and `decode` accept empty strings.
   - Empty input returns empty output without error.

2. **Binary Data**
   - The module handles binary data (strings with null bytes and non-ASCII characters).
   - Binary data is preserved during encode/decode round-trips.

3. **Padding**
   - Standard Base64 padding with `=` characters is used.
   - Decoding requires proper padding.

## Thread Safety

- The `base64` module is thread-safe.
- It uses immutable module tables shared across Lua states.
- No internal mutable state is maintained.

## Module Classification

- **Class**: `encoding`, `deterministic`
- Operations are pure functions with no side effects.
- Same input always produces the same output.

## Example Usage

```lua
local base64 = require("base64")

-- Basic encoding
local encoded = base64.encode("hello")
print(encoded)  -- "aGVsbG8="

-- Basic decoding
local decoded = base64.decode("aGVsbG8=")
print(decoded)  -- "hello"

-- Round-trip
local original = "binary\x00data\xff"
local encoded = base64.encode(original)
local decoded = base64.decode(encoded)
assert(decoded == original)  -- true

-- Error handling with kind constants
local result, err = base64.decode("!!!invalid!!!")
if err then
    print("Decode failed:", err:message())

    -- Use constants for kind comparison
    if err:kind() == errors.INVALID then
        print("Invalid input provided")
    end

    print("Retryable:", err:retryable())  -- false
    print("Full error: " .. err)
end

-- Empty string handling
local empty_encoded = base64.encode("")
local empty_decoded = base64.decode("")
assert(empty_encoded == "")
assert(empty_decoded == "")
```

## Implementation Notes

- Uses Go's `encoding/base64.StdEncoding` (standard Base64 alphabet).
- Module uses `ModuleDef` struct for definition.
- Module table is created once and shared across all Lua states via `sync.Once`.
- Errors include Lua stack traces for debugging.

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "base64",
    Description: "Base64 encoding and decoding",
    Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
    Build: func() (*lua.LTable, []luaapi.YieldType) {
        mod := &lua.LTable{}
        mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
        mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
        mod.Immutable = true
        return mod, []luaapi.YieldType{}
    },
}
```
