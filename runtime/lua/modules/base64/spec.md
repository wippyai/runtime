# base64

Base64 encoding and decoding. Encoding, deterministic.

## Loading

```lua
local base64 = require("base64")
```

## Functions

### encode(data: string) → string, error

Encodes string to Base64 (RFC 4648 standard encoding with padding).

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to encode (supports binary) |

**Returns:** `string` - Base64 encoded string, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |

**Notes:**
- Empty string input returns empty string (no error)
- Binary data (null bytes, non-ASCII) is supported

### decode(data: string) → string, error

Decodes Base64 string to original data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Base64 encoded string |

**Returns:** `string` - Decoded string, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Invalid Base64 data | errors.INVALID | no |

**Notes:**
- Empty string input returns empty string (no error)
- Requires proper padding (`=` characters)

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = base64.decode(input)
if err then
    if err:kind() == errors.INVALID then
        -- bad input type or malformed base64
    end
end
```

**Possible kinds:** `errors.INVALID`

## Example

```lua
local base64 = require("base64")

local encoded, err = base64.encode("hello")
if err then error(err) end
print(encoded)  -- "aGVsbG8="

local decoded, err = base64.decode(encoded)
if err then error(err) end
print(decoded)  -- "hello"

-- Binary data round-trip
local binary = "\x00\x01\x02"
local enc = base64.encode(binary)
local dec = base64.decode(enc)
assert(dec == binary)
```
