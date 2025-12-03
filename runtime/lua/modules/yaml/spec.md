# Lua YAML Module Specification

## Overview

The `yaml` module provides YAML encoding and decoding functions for Lua tables. It uses YAML 1.1/1.2 compatible parsing.

## Module Interface

### Module Loading

```lua
local yaml = require("yaml")
```

### Functions

#### yaml.encode(data: table)

Encodes a Lua table to YAML format.

Parameters:

- `data`: Lua table to encode.

Returns:

- `encoded`: YAML string (or nil on error).
- `error`: Structured error object (or nil on success).

#### yaml.decode(data: string)

Decodes a YAML string to a Lua table.

Parameters:

- `data`: YAML string to decode.

Returns:

- `decoded`: Lua table (or nil on error).
- `error`: Structured error object (or nil on success).

## Error Handling

The module returns structured errors using the `lua.Error` type.

### Error Types

1. **Invalid Input Type:** If input is not the expected type.

```lua
local result, err = yaml.encode(123)
-- result: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
-- tostring(err) == "table expected"

local result, err = yaml.decode(123)
-- result: nil
-- err:kind() == errors.INVALID
-- tostring(err) == "string expected"
```

2. **Empty Input:** If decode input is empty.

```lua
local result, err = yaml.decode("")
-- result: nil
-- err:kind() == errors.INVALID
-- tostring(err) == "input cannot be empty"
```

3. **Invalid YAML Data:** If input contains invalid YAML syntax.

```lua
local result, err = yaml.decode(":\n  :\n  invalid")
-- result: nil
-- err:kind() == errors.INTERNAL
-- err:retryable() == false
```

### Error Kind Comparison

Always use `errors.*` constants for kind comparison:

```lua
local result, err = yaml.decode(input)
if err then
    if err:kind() == errors.INVALID then
        -- handle invalid input
    elseif err:kind() == errors.INTERNAL then
        -- handle parse error
    end
end
```

## Behavior

1. **Table Encoding**
   - Both array-style and map-style tables are supported.
   - Nested tables are encoded recursively.

2. **Empty Input Handling**
   - `encode` requires a table input.
   - `decode` does not accept empty strings.

3. **Type Preservation**
   - Numbers, strings, booleans, and nil are preserved.
   - Nested structures maintain their hierarchy.

## Thread Safety

- The `yaml` module is thread-safe.
- It uses immutable module tables shared across Lua states.
- No internal mutable state is maintained.

## Module Classification

- **Class**: `encoding`, `deterministic`
- Operations are pure functions with no side effects.
- Same input always produces the same output.

## Example Usage

```lua
local yaml = require("yaml")

-- Basic encoding
local encoded, err = yaml.encode({name = "test", value = 123})
if err then
    print("Error:", err)
else
    print(encoded)
end
-- Output:
-- name: test
-- value: 123

-- Basic decoding
local decoded, err = yaml.decode("name: test\nvalue: 123")
if err then
    print("Error:", err)
else
    print(decoded.name)   -- "test"
    print(decoded.value)  -- 123
end

-- Array encoding
local arr_encoded = yaml.encode({1, 2, 3})
-- Output:
-- - 1
-- - 2
-- - 3

-- Nested structures
local nested = yaml.decode([[
parent:
  child:
    value: 123
]])
print(nested.parent.child.value)  -- 123

-- Round-trip
local original = {name = "test", items = {1, 2, 3}}
local encoded = yaml.encode(original)
local decoded = yaml.decode(encoded)
assert(decoded.name == original.name)

-- Error handling with kind constants
local result, err = yaml.decode("invalid: :")
if err then
    if err:kind() == errors.INTERNAL then
        print("Parse error:", err)
    end
end
```

## Implementation Notes

- Uses Go's `gopkg.in/yaml.v3` library.
- Module uses `ModuleDef` struct for definition.
- Module table is created once and shared across all Lua states.
- Errors include Lua stack traces for debugging.

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "yaml",
    Description: "YAML encoding and decoding",
    Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
    Build: func() (*lua.LTable, []luaapi.YieldType) {
        mod := lua.CreateTable(0, 2)
        mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
        mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
        mod.Immutable = true
        return mod, nil
    },
}
```
