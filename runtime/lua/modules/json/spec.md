# Lua JSON Module Specification

## Overview

The `json` module provides functions for encoding Lua values into JSON strings and decoding JSON strings into Lua values. It handles basic Lua types (strings, numbers, booleans, nil) as well as tables used as arrays or objects. The module also supports JSON Schema validation.

## Module Interface

### Module Loading

```lua
local json = require("json")
```

### Functions

#### json.encode(value: any)

Encodes a Lua value into a JSON string.

Parameters:
- `value`: The Lua value to encode.

Returns:
- `encoded`: The JSON string representation of the value (or nil on error).
- `error`: Structured error (or nil on success).

#### json.decode(str: string)

Decodes a JSON string into a Lua value.

Parameters:
- `str`: The JSON string to decode.

Returns:
- `decoded`: The Lua value represented by the JSON string (or nil on error).
- `error`: Structured error (or nil on success).

#### json.validate(schema: table|string, data: any)

Validates a Lua value against a JSON schema.

Parameters:
- `schema`: The JSON schema as a Lua table or JSON string.
- `data`: The Lua value to validate against the schema.

Returns:
- `valid`: Boolean `true` if validation succeeds, `false` on error.
- `error`: Structured error (or nil on success).

#### json.validate_string(schema: table|string, json_str: string)

Validates a JSON string against a JSON schema without decoding it first.

Parameters:
- `schema`: The JSON schema as a Lua table or JSON string.
- `json_str`: The JSON string to validate.

Returns:
- `valid`: Boolean `true` if validation succeeds, `false` on error.
- `error`: Structured error (or nil on success).

## Error Handling

All errors are returned as structured error objects with:
- `message`: Human-readable error description
- `kind`: Error kind (e.g., "invalid", "internal")
- `retryable`: Boolean indicating if the operation can be retried

Common error scenarios:

1. **Invalid Input Type**: Non-string input to `decode`
2. **Empty String**: Empty string input to `decode`
3. **Invalid JSON**: Malformed JSON string
4. **Recursive Tables**: Tables containing themselves
5. **Sparse Arrays**: Non-contiguous numeric keys
6. **Mixed Keys**: Tables with both numeric and string keys

## Behavior

### Encoding

- `nil` encodes as `null`
- Booleans encode as `true` or `false`
- Numbers encode as JSON numbers
- Strings encode as JSON strings with proper escaping
- Tables with sequential numeric keys (1-based) encode as arrays
- Tables with string keys encode as objects
- Nested tables are supported (no recursion allowed)

### Decoding

- `null` decodes as `nil`
- JSON booleans decode as Lua booleans
- JSON numbers decode as Lua numbers
- JSON strings decode as Lua strings
- JSON arrays decode as 1-indexed Lua tables
- JSON objects decode as Lua tables with string keys

## Example Usage

```lua
local json = require("json")

-- Encode a table
local data = {name = "John", age = 30, active = true}
local encoded, err = json.encode(data)
if err then
    print("Error:", err.message)
else
    print(encoded) -- {"name":"John","age":30,"active":true}
end

-- Decode JSON
local decoded, err = json.decode('{"name":"Jane","items":[1,2,3]}')
if decoded then
    print(decoded.name)     -- Jane
    print(decoded.items[1]) -- 1
end

-- Validate with schema
local schema = {
    type = "object",
    properties = {
        name = {type = "string"},
        age = {type = "number", minimum = 0}
    },
    required = {"name"}
}

local valid, err = json.validate(schema, {name = "John", age = 30})
if valid then
    print("Data is valid")
end

-- Validate JSON string directly
local valid, err = json.validate_string(schema, '{"name":"Jane","age":25}')
```
