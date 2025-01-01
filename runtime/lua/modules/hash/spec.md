# Lua Hash Module Specification

## Overview

The `hash` module provides a set of functions for calculating various cryptographic and non-cryptographic hash values of
strings. It includes implementations of MD5, SHA-1, SHA-256, SHA-512, FNV-1 32-bit, and FNV-1 64-bit hash algorithms.

## Module Interface

### Module Loading

```lua
local hash = require("hash")
```

### Global Functions

#### hash.md5(str: string)

Calculates the MD5 hash of a string.

Parameters:

- `str`: The string to hash.

Returns:

- `digest`: The hexadecimal representation of the MD5 hash digest (or nil on error).
- `error`: Error message string (or nil on success).

#### hash.sha1(str: string)

Calculates the SHA-1 hash of a string.

Parameters:

- `str`: The string to hash.

Returns:

- `digest`: The hexadecimal representation of the SHA-1 hash digest (or nil on error).
- `error`: Error message string (or nil on success).

#### hash.sha256(str: string)

Calculates the SHA-256 hash of a string.

Parameters:

- `str`: The string to hash.

Returns:

- `digest`: The hexadecimal representation of the SHA-256 hash digest (or nil on error).
- `error`: Error message string (or nil on success).

#### hash.sha512(str: string)

Calculates the SHA-512 hash of a string.

Parameters:

- `str`: The string to hash.

Returns:

- `digest`: The hexadecimal representation of the SHA-512 hash digest (or nil on error).
- `error`: Error message string (or nil on success).

#### hash.fnv32(str: string)

Calculates the FNV-1 32-bit hash of a string.

Parameters:

- `str`: The string to hash.

Returns:

- `value`: The 32-bit FNV-1 hash value as a number (or nil on error).
- `error`: Error message string (or nil on success).

#### hash.fnv64(str: string)

Calculates the FNV-1 64-bit hash of a string.

Parameters:

- `str`: The string to hash.

Returns:

- `value`: The 64-bit FNV-1 hash value as a number (or nil on error).
- `error`: Error message string (or nil on success).

## Error Handling

The module functions return errors in the following cases:

1. **Invalid Input Type:** If the input to any hash function is not a string.

    ```lua
    local digest, err = hash.md5(123)  -- digest: nil, err: "string expected"
    ```

2. **Internal Errors:** If an error occurs during the hash computation, the function will return `nil` and an error
   message.

## Behavior

1. **Hash Output:**
    - `md5`, `sha1`, `sha256`, and `sha512` return the hash digest as a hexadecimal string.
    - `fnv32` and `fnv64` return the hash value as a Lua number.

2. **Empty String:**
    - Hashing an empty string with `md5`, `sha1`, `sha256`, and `sha512` will return the pre-computed hash digest of an
      empty string.
    - Hashing an empty string with `fnv32` and `fnv64` will return a number.

3. **Error Handling**
    - Lua's `pcall` can be used to catch errors that occur.

## Thread Safety

- The `hash` module is designed to be thread-safe.
- It does not maintain any internal state that could be affected by concurrent access.

## Best Practices

1. **Always check for errors:** Always check the returned `error` value to handle potential errors.
2. **Validate input:** Ensure that the input to the hash functions is of the correct type (string) before calling them.
3. **Use appropriate hash function:** Choose the hash function that best suits your needs in terms of security and
   performance.
4. **Handle empty strings:** Be aware that hashing empty strings produces defined results.

## Example Usage

```lua
local hash = require("hash")

-- Calculate MD5 hash
local digest, err = hash.md5("Hello, world!")
if err then
  print("MD5 Error:", err)
else
  print("MD5:", digest) -- Output: MD5: 65a8e27d8879283831b664bd8b7f0ad4
end

-- Calculate SHA-256 hash
local digest, err = hash.sha256("Hello, world!")
if err then
  print("SHA-256 Error:", err)
else
  print("SHA-256:", digest) -- Output: SHA-256: 315f5bdb76d078c43b8ac0064e4a0164612b1fce77c869345bfc94c75894edd3
end

-- Calculate FNV-1 32-bit hash
local value, err = hash.fnv32("Hello, world!")
if err then
  print("FNV32 Error:", err)
else
  print("FNV32:", value) -- Output: FNV32: <a number>
end

-- Handle errors with pcall
local success, result = pcall(hash.sha1, 123)
if not success then
    print("Error:", result) -- Output: Error: string expected
else
    print("SHA1 result:", result)
end

-- Test empty string hashing
local empty_md5, err = hash.md5("")
if err then
    print("Empty MD5 Error:", err)
else
    print("Empty MD5:", empty_md5) -- Output: Empty MD5: d41d8cd98f00b204e9800998ecf8427e
end
```
