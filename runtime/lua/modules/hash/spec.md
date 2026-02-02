# hash

Cryptographic hash functions and HMAC. Encoding, security, deterministic.

## Loading

```lua
local hash = require("hash")
```

## Functions

### md5(data: string, raw?: boolean) → string, error

Computes MD5 hash of input data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to hash |
| raw | boolean | no | false | If true, returns raw bytes; if false, returns hex string |

**Returns:**
- Success: hex string (32 chars) or raw bytes (16 bytes)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |

### sha1(data: string, raw?: boolean) → string, error

Computes SHA-1 hash of input data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to hash |
| raw | boolean | no | false | If true, returns raw bytes; if false, returns hex string |

**Returns:**
- Success: hex string (40 chars) or raw bytes (20 bytes)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |

### sha256(data: string, raw?: boolean) → string, error

Computes SHA-256 hash of input data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to hash |
| raw | boolean | no | false | If true, returns raw bytes; if false, returns hex string |

**Returns:**
- Success: hex string (64 chars) or raw bytes (32 bytes)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |

### sha512(data: string, raw?: boolean) → string, error

Computes SHA-512 hash of input data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to hash |
| raw | boolean | no | false | If true, returns raw bytes; if false, returns hex string |

**Returns:**
- Success: hex string (128 chars) or raw bytes (64 bytes)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |

### fnv32(data: string) → number, error

Computes FNV-1a 32-bit hash of input data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to hash |

**Returns:**
- Success: number (32-bit unsigned integer)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |

### fnv64(data: string) → number, error

Computes FNV-1a 64-bit hash of input data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to hash |

**Returns:**
- Success: number (64-bit unsigned integer)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |

### hmac_sha256(data: string, secret: string, raw?: boolean) → string, error

Computes HMAC-SHA256 of input data with secret key.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to authenticate |
| secret | string | yes | - | Secret key for HMAC |
| raw | boolean | no | false | If true, returns raw bytes; if false, returns hex string |

**Returns:**
- Success: hex string (64 chars) or raw bytes (32 bytes)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |
| secret not a string | errors.INVALID | no |

### hmac_sha512(data: string, secret: string, raw?: boolean) → string, error

Computes HMAC-SHA512 of input data with secret key.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to authenticate |
| secret | string | yes | - | Secret key for HMAC |
| raw | boolean | no | false | If true, returns raw bytes; if false, returns hex string |

**Returns:**
- Success: hex string (128 chars) or raw bytes (64 bytes)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |
| secret not a string | errors.INVALID | no |

### hmac_sha1(data: string, secret: string, raw?: boolean) → string, error

Computes HMAC-SHA1 of input data with secret key.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to authenticate |
| secret | string | yes | - | Secret key for HMAC |
| raw | boolean | no | false | If true, returns raw bytes; if false, returns hex string |

**Returns:**
- Success: hex string (40 chars) or raw bytes (20 bytes)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |
| secret not a string | errors.INVALID | no |

### hmac_md5(data: string, secret: string, raw?: boolean) → string, error

Computes HMAC-MD5 of input data with secret key.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to authenticate |
| secret | string | yes | - | Secret key for HMAC |
| raw | boolean | no | false | If true, returns raw bytes; if false, returns hex string |

**Returns:**
- Success: hex string (32 chars) or raw bytes (16 bytes)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| data not a string | errors.INVALID | no |
| secret not a string | errors.INVALID | no |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = hash.sha256(input)
if err then
    if err:kind() == errors.INVALID then
        -- input was not a string
    end
end
```

**Possible kinds:** `errors.INVALID`

## Example

```lua
local hash = require("hash")

-- Basic hashing
local digest, err = hash.sha256("hello")
if err then error(err) end
print(digest)  -- "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

-- Raw bytes output
local raw = hash.sha256("hello", true)
print(#raw)  -- 32

-- HMAC authentication
local mac = hash.hmac_sha256("message", "secret-key")
print(#mac)  -- 64

-- FNV hashing (returns number)
local fnv = hash.fnv32("hello")
print(type(fnv))  -- "number"

-- All hash functions are deterministic
assert(hash.md5("test") == hash.md5("test"))
```
