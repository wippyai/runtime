<!-- SPDX-License-Identifier: MPL-2.0 -->

# toml

TOML encoding and decoding. Encoding, deterministic.

## Loading

```lua
local toml = require("toml")
```

## Functions

### encode(data: table) → string, error

Encodes a Lua table to a TOML document string.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | table | yes | - | Table to encode; TOML requires a map at the document root |

**Returns:**
- Success: `string` - TOML formatted string
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not table | errors.INVALID | no |
| data missing | errors.INVALID | no |
| output above 512 KiB | errors.INVALID | no |
| encode failed | errors.INTERNAL | no |

### decode(data: string) → table, error

Decodes a TOML document string to a Lua table.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | TOML string to decode, at most 256 KiB |

**Returns:**
- Success: `table` - decoded Lua table
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not string | errors.INVALID | no |
| data empty | errors.INVALID | no |
| data above 256 KiB | errors.INVALID | no |
| invalid TOML syntax | errors.INTERNAL | no |
| decode failed | errors.INTERNAL | no |
| convert to Lua failed | errors.INTERNAL | no |

## Types

| TOML | Lua |
|------|-----|
| table | table with string keys |
| array, array of tables | table with integer keys from 1 |
| integer | integer |
| float | number |
| boolean | boolean |
| string | string |
| offset date-time, local date-time, local date, local time | string |

A Lua table whose `#` length is above zero encodes as a TOML array, otherwise as
a TOML table. Lua `nil` is absent from a table, so it never reaches the encoder.

### Dates and times

`decode` renders the four TOML date and time types as RFC 3339 text: an offset
date-time keeps its offset, a local date-time, local date and local time carry
the corresponding RFC 3339 fragment. `encode` writes strings as TOML strings and
never synthesizes a date or time from text, so a decode followed by an encode
turns a date into a quoted string.

## Limits

`decode` rejects input above 256 KiB and `encode` rejects output above 512 KiB.
`github.com/pelletier/go-toml/v2` offers no pre-parse structural limit, so
nesting and entry counts are bounded only by these byte limits.

## Not supported

- Comments and key order from the source document; `encode` writes keys in sorted
  order and drops comments.
- Encoding options such as field order or inline tables.
- A TOML array at the document root, which TOML itself disallows.

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = toml.decode(input)
if err then
    if err:kind() == errors.INVALID then
        -- bad input type, empty string or byte limit
    elseif err:kind() == errors.INTERNAL then
        -- TOML parse error or internal failure
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local toml = require("toml")

-- Encode table to TOML
local encoded, err = toml.encode({name = "test", value = 123})
if err then error(err) end
-- Result: "name = 'test'\nvalue = 123\n"

-- Decode TOML to table
local decoded, err = toml.decode("name = 'test'\nvalue = 123")
if err then error(err) end
print(decoded.name)   -- "test"
print(decoded.value)  -- 123

-- Nested tables
local nested = toml.decode([[
[parent.child]
value = 123
]])
print(nested.parent.child.value)  -- 123

-- Add a provider entry without replacing an existing one
local config, err = toml.decode(existing)
if err then error(err) end
config.mcp_servers = config.mcp_servers or {}
if config.mcp_servers.bee then error("mcp_servers.bee already exists") end
config.mcp_servers.bee = {url = "http://127.0.0.1:4321/mcp/action"}
local out, err = toml.encode(config)
if err then error(err) end

-- Error handling
local result, err = toml.decode(123)
if err then
    if err:kind() == errors.INVALID then
        print("Invalid input type")
    end
end
```
