# json

JSON encoding and decoding with schema validation. Encoding, deterministic.

## Loading

```lua
local json = require("json")
```

## Functions

### encode(value: any) → string

Encodes a Lua value into a JSON string.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| value | any | yes | - | Any Lua value (nil, boolean, number, string, table) |

**Returns:** `string` - JSON string representation (nil encodes as "null")

**Errors (structured):**

| Condition | Kind | Retryable | Notes |
|-----------|------|-----------|-------|
| recursively nested table | errors.INTERNAL | no | Table contains itself |
| sparse array | errors.INTERNAL | no | Non-contiguous numeric keys (e.g., {[1]="a", [3]="c"}) |
| mixed-key table | errors.INTERNAL | no | Table has both numeric and non-numeric keys |
| max depth exceeded | errors.INTERNAL | no | Exceeds 128 levels of nesting |

**Encoding rules:**
- `nil` → `"null"`
- Booleans → `"true"` or `"false"`
- Numbers → JSON numbers (Inf/NaN → `"null"`)
- Strings → JSON strings with escaping
- Tables with sequential numeric keys (1-based) → JSON arrays
- Tables with string keys → JSON objects
- Empty tables → `"[]"`

### decode(str: string) → any, error

Decodes a JSON string into a Lua value.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| str | string | yes | - | JSON string to decode |

**Returns:**
- Success: Lua value (nil for "null")
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable | Notes |
|-----------|------|-----------|-------|
| input not string | errors.INVALID | no | Non-string argument |
| empty string | errors.INVALID | no | Empty string is not valid JSON |
| invalid JSON | errors.INTERNAL | no | Malformed JSON syntax |

**Decoding rules:**
- `"null"` → `nil`
- JSON booleans → Lua booleans
- JSON numbers → Lua numbers
- JSON strings → Lua strings
- JSON arrays → 1-indexed Lua tables
- JSON objects → Lua tables with string keys

### validate(schema: table | string, data: any) → boolean, error

Validates a Lua value against a JSON Schema.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| schema | table \| string | yes | - | JSON Schema as Lua table or JSON string |
| data | any | yes | - | Lua value to validate |

**Returns:**
- Success: `true` - data is valid
- Error: `false, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable | Notes |
|-----------|------|-----------|-------|
| schema is nil | errors.INVALID | no | Schema parameter required |
| data is nil | errors.INVALID | no | Data parameter required |
| schema invalid | errors.INVALID | no | Schema not table or string |
| schema compile error | errors.INVALID | no | Invalid JSON Schema |
| validation failed | errors.INVALID | no | Data doesn't match schema |

**Schema caching:** Schemas are cached internally by SHA256 hash (cache size: 100).

### validate_string(schema: table | string, json_str: string) → boolean, error

Validates a JSON string against a JSON Schema without decoding it first.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| schema | table \| string | yes | - | JSON Schema as Lua table or JSON string |
| json_str | string | yes | - | JSON string to validate |

**Returns:**
- Success: `true` - JSON string is valid
- Error: `false, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable | Notes |
|-----------|------|-----------|-------|
| schema is nil | errors.INVALID | no | Schema parameter required |
| data not string | errors.INVALID | no | Second parameter must be string |
| schema invalid | errors.INVALID | no | Schema not table or string |
| schema compile error | errors.INVALID | no | Invalid JSON Schema |
| validation failed | errors.INVALID | no | JSON doesn't match schema |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = json.decode("invalid")
if err then
    if err:kind() == errors.INVALID then
        -- bad input
    elseif err:kind() == errors.INTERNAL then
        -- encoding/decoding failed
    end
    print(err:message())
    print(err:retryable())  -- always false for this module
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local json = require("json")

-- Encode a table to JSON
local data = {name = "John", age = 30, active = true}
local encoded = json.encode(data)
-- Result: {"name":"John","age":30,"active":true} (order may vary)

-- Decode JSON to Lua table
local decoded, err = json.decode('{"name":"Jane","items":[1,2,3]}')
if err then
    error(err:message())
end
print(decoded.name)     -- "Jane"
print(decoded.items[1]) -- 1

-- Validate data against schema
local schema = {
    type = "object",
    properties = {
        name = {type = "string"},
        age = {type = "number", minimum = 0}
    },
    required = {"name"}
}

local valid, err2 = json.validate(schema, {name = "John", age = 30})
if not valid then
    error(err2:message())
end

-- Validate JSON string directly
local valid2, err3 = json.validate_string(schema, '{"name":"Jane","age":25}')
if not valid2 then
    error(err3:message())
end

-- Round-trip encode/decode
local original = {items = {1, 2, 3}, meta = {ok = true}}
local json_str = json.encode(original)
local restored = json.decode(json_str)
```
