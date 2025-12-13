# yaml

YAML encoding and decoding. Encoding, deterministic.

## Loading

```lua
local yaml = require("yaml")
```

## Functions

### encode(data: table) → string, error

Encodes a Lua table to YAML format string.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | table | yes | - | Table to encode (arrays and maps supported) |

**Returns:**
- Success: `string` - YAML formatted string
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not table | errors.INVALID | no |
| data missing | errors.INVALID | no |
| encode failed | errors.INTERNAL | no |

### decode(data: string) → table, error

Decodes a YAML string to a Lua table.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | YAML string to decode |

**Returns:**
- Success: `table` - decoded Lua table
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not string | errors.INVALID | no |
| data empty | errors.INVALID | no |
| invalid YAML syntax | errors.INTERNAL | no |
| decode failed | errors.INTERNAL | no |
| convert to Lua failed | errors.INTERNAL | no |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = yaml.decode(input)
if err then
    if err:kind() == errors.INVALID then
        -- bad input type or empty string
    elseif err:kind() == errors.INTERNAL then
        -- YAML parse error or internal failure
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local yaml = require("yaml")

-- Encode table to YAML
local encoded, err = yaml.encode({name = "test", value = 123})
if err then error(err) end
-- Result: "name: test\nvalue: 123\n"

-- Decode YAML to table
local decoded, err = yaml.decode("name: test\nvalue: 123")
if err then error(err) end
print(decoded.name)   -- "test"
print(decoded.value)  -- 123

-- Arrays
local arr_encoded, err = yaml.encode({1, 2, 3})
if err then error(err) end
-- Result: "- 1\n- 2\n- 3\n"

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

-- Error handling
local result, err = yaml.decode(123)
if err then
    if err:kind() == errors.INVALID then
        print("Invalid input type")
    end
end
```
