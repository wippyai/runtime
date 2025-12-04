# Lua Hash Module Specification

## Overview

The `hash` module provides cryptographic hash functions and HMAC. This module is loaded as part of the `crypto` component.

## Module Interface

```lua
local hash = require("hash")
local digest = hash.sha256("hello")
```

## Hash Functions

### hash.md5(data, raw)

Computes MD5 hash.

Parameters:

- `data`: String to hash.
- `raw`: Optional boolean. If true, returns raw bytes instead of hex string.

Returns:

- Hex string (32 chars) or raw bytes (16 bytes).

```lua
local hex = hash.md5("hello")
-- hex: "5d41402abc4b2a76b9719d911017c592"

local raw = hash.md5("hello", true)
-- raw: 16-byte binary string
```

### hash.sha1(data, raw)

Computes SHA-1 hash.

Parameters:

- `data`: String to hash.
- `raw`: Optional boolean. If true, returns raw bytes.

Returns:

- Hex string (40 chars) or raw bytes (20 bytes).

```lua
local hex = hash.sha1("hello")
-- hex: "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"
```

### hash.sha256(data, raw)

Computes SHA-256 hash.

Parameters:

- `data`: String to hash.
- `raw`: Optional boolean. If true, returns raw bytes.

Returns:

- Hex string (64 chars) or raw bytes (32 bytes).

```lua
local hex = hash.sha256("hello")
-- hex: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
```

### hash.sha512(data, raw)

Computes SHA-512 hash.

Parameters:

- `data`: String to hash.
- `raw`: Optional boolean. If true, returns raw bytes.

Returns:

- Hex string (128 chars) or raw bytes (64 bytes).

```lua
local hex = hash.sha512("hello")
-- hex: 128-character hex string
```

### hash.fnv32(data)

Computes FNV-1a 32-bit hash.

Parameters:

- `data`: String to hash.

Returns:

- Number (32-bit unsigned integer).

```lua
local num = hash.fnv32("hello")
-- num: 1335831723
```

### hash.fnv64(data)

Computes FNV-1a 64-bit hash.

Parameters:

- `data`: String to hash.

Returns:

- Number (64-bit unsigned integer).

```lua
local num = hash.fnv64("hello")
-- num: 11831194018420276491
```

## HMAC Functions

### hash.hmac_sha256(data, secret, raw)

Computes HMAC-SHA256.

Parameters:

- `data`: String to authenticate.
- `secret`: Secret key string.
- `raw`: Optional boolean. If true, returns raw bytes.

Returns:

- Hex string (64 chars) or raw bytes (32 bytes).

```lua
local hmac = hash.hmac_sha256("hello", "secret")
-- hmac: 64-character hex string
```

### hash.hmac_sha512(data, secret, raw)

Computes HMAC-SHA512.

Parameters:

- `data`: String to authenticate.
- `secret`: Secret key string.
- `raw`: Optional boolean. If true, returns raw bytes.

Returns:

- Hex string (128 chars) or raw bytes (64 bytes).

```lua
local hmac = hash.hmac_sha512("hello", "secret")
-- hmac: 128-character hex string
```

### hash.hmac_sha1(data, secret, raw)

Computes HMAC-SHA1.

Parameters:

- `data`: String to authenticate.
- `secret`: Secret key string.
- `raw`: Optional boolean. If true, returns raw bytes.

Returns:

- Hex string (40 chars) or raw bytes (20 bytes).

```lua
local hmac = hash.hmac_sha1("hello", "secret")
-- hmac: 40-character hex string
```

### hash.hmac_md5(data, secret, raw)

Computes HMAC-MD5.

Parameters:

- `data`: String to authenticate.
- `secret`: Secret key string.
- `raw`: Optional boolean. If true, returns raw bytes.

Returns:

- Hex string (32 chars) or raw bytes (16 bytes).

```lua
local hmac = hash.hmac_md5("hello", "secret")
-- hmac: 32-character hex string
```

## Error Handling

All functions throw a Lua error if input is not a string. This is consistent with v1 behavior.

```lua
-- This throws an error:
hash.sha256(123)  -- bad argument #1 (string expected)

-- Use pcall for safe handling:
local ok, result = pcall(hash.sha256, input)
if not ok then
    print("Error:", result)
end
```

## Module Classification

- **Class**: `encoding`, `security`, `deterministic`
- All functions are deterministic: same input always produces the same output.

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "hash",
    Description: "Cryptographic hash functions and HMAC",
    Class:       []string{luaapi.ClassEncoding, luaapi.ClassSecurity, luaapi.ClassDeterministic},
    Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
    mod := lua.CreateTable(0, 10)
    mod.RawSetString("md5", lua.LGoFunc(hashMD5))
    mod.RawSetString("sha1", lua.LGoFunc(hashSHA1))
    mod.RawSetString("sha256", lua.LGoFunc(hashSHA256))
    mod.RawSetString("sha512", lua.LGoFunc(hashSHA512))
    mod.RawSetString("fnv32", lua.LGoFunc(hashFNV32))
    mod.RawSetString("fnv64", lua.LGoFunc(hashFNV64))
    mod.RawSetString("hmac_sha256", lua.LGoFunc(hmacSHA256))
    mod.RawSetString("hmac_sha512", lua.LGoFunc(hmacSHA512))
    mod.RawSetString("hmac_sha1", lua.LGoFunc(hmacSHA1))
    mod.RawSetString("hmac_md5", lua.LGoFunc(hmacMD5))
    mod.Immutable = true
    return mod, nil
}
```
