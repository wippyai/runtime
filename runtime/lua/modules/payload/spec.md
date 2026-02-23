<!-- SPDX-License-Identifier: MPL-2.0 -->

# payload

Data transcoding and format conversion. Encoding, deterministic.

## Constants

```lua
payload.format.JSON     -- "json/plain"
payload.format.YAML     -- "yaml/plain"
payload.format.STRING   -- "text/plain"
payload.format.BYTES    -- "application/octet-stream"
payload.format.MSGPACK  -- "application/msgpack"
payload.format.LUA      -- "lua/any"
payload.format.GOLANG   -- "golang/any"
payload.format.ERROR    -- "golang/error"
```

## Functions

### new(value: any) -> Payload

Creates a new payload from a Lua value.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| value | any | yes | - | string, number, boolean, table, nil, or error |

**Returns:** `Payload` - wrapper with format `payload.format.LUA` (or `payload.format.ERROR` if value is an error)

```lua
local p = payload.new({key = "value"})
local err_p = payload.new(errors.new("failed"))  -- format = ERROR
```

## Types

### Payload

Returned by `payload.new()` or received from other modules.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| get_format | () | string | One of `payload.format.*` values |
| data | () | value, error | Extracts Lua value, transcodes if needed |
| unmarshal | () | value, error | Same as `data()` |
| transcode | (format: string) | Payload, error | Converts to target format |
| tostring | () | string | `"payload{format=...}"` |

#### p:get_format() -> string

Returns the payload format.

**Returns:** One of `payload.format.*` constant values.

#### p:data() -> any, error

Extracts the Lua value from the payload.

- For `payload.format.LUA`: Returns original value directly
- For other formats: Transcodes to Lua format first using context transcoder

**Returns:**
- Success: `value` - the Lua value
- Error: `nil, error` - structured error if transcoding fails

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Transcoding failure | errors.INTERNAL | no |

#### p:unmarshal() -> any, error

Same as `data()`. Unmarshals payload to Lua value.

**Returns:**
- Success: `value` - the Lua value
- Error: `nil, error` - structured error if transcoding fails

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Transcoding failure | errors.INTERNAL | no |
| Result not valid Lua value | errors.INTERNAL | no |

#### p:transcode(format: string) -> Payload, error

Transcodes payload to a different format.

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| format | string | yes | Target format from `payload.format.*` |

**Returns:**
- Success: `Payload, nil` - new payload in target format
- Error: `nil, error` - structured error if transcoding fails

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Transcoding failure | errors.INTERNAL | no |

```lua
local json_p, err = p:transcode(payload.format.JSON)
if err then
    print(err:kind())  -- errors.INTERNAL
end
```

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local data, err = p:data()
if err then
    if err:kind() == errors.INTERNAL then
        -- transcoding failed
    end
end
```

**Possible kinds:** `errors.INTERNAL`

## Example

```lua
-- Create payload from table
local p = payload.new({
    name = "test",
    values = {1, 2, 3}
})

-- Check format
print(p:get_format())  -- "lua/any"

-- Get data back
local data = p:data()
print(data.name)  -- "test"

-- Transcode to JSON
local json_p, err = p:transcode(payload.format.JSON)
if not err then
    print(json_p:get_format())  -- "json/plain"
end

-- Common types
local str_p = payload.new("hello")
local num_p = payload.new(123)
local bool_p = payload.new(true)
local nil_p = payload.new(nil)

-- Error payloads
local err_p = payload.new(errors.new("failed"))
print(err_p:get_format())  -- "golang/error"
```
