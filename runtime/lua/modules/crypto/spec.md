# Lua Crypto Module Specification

## Overview

The `crypto` module provides cryptographic functions for secure implementations of OAuth 2.0 and other security-related
applications. It includes submodules for random data generation, HMAC computation, symmetric encryption/decryption, JWT
handling, and utility functions.

## Module Interface

### Module Loading

```lua
local crypto = require("crypto")
```

### Submodules and Functions

#### Random Data Generation

##### crypto.random.bytes(length: number)

Generates cryptographically secure random bytes.

Parameters:

- `length`: Number of random bytes to generate.

Returns:

- `bytes`: Random bytes as a string (or nil on error).
- `error`: Error message string (or nil on success).

##### crypto.random.string(length: number, charset: string)

Generates a random string using the specified character set.

Parameters:

- `length`: Length of the string to generate.
- `charset`: (Optional) Characters to use (default: alphanumeric).

Returns:

- `str`: Random string (or nil on error).
- `error`: Error message string (or nil on success).

##### crypto.random.uuid()

Generates a random UUID (v4).

Returns:

- `uuid`: UUID string (or nil on error).
- `error`: Error message string (or nil on success).

#### HMAC Functions

##### crypto.hmac.sha256(key: string, data: string)

Calculates HMAC-SHA256.

Parameters:

- `key`: HMAC key.
- `data`: Data to authenticate.

Returns:

- `digest`: Hex-encoded HMAC digest (or nil on error).
- `error`: Error message string (or nil on success).

##### crypto.hmac.sha512(key: string, data: string)

Calculates HMAC-SHA512.

Parameters:

- `key`: HMAC key.
- `data`: Data to authenticate.

Returns:

- `digest`: Hex-encoded HMAC digest (or nil on error).
- `error`: Error message string (or nil on success).

#### Encryption Functions

##### crypto.encrypt.aes(data: string, key: string, aad: string)

Encrypts data using AES-GCM (authenticated encryption).

Parameters:

- `data`: Data to encrypt.
- `key`: Encryption key (16, 24, or 32 bytes).
- `aad`: (Optional) Additional authenticated data.

Returns:

- `encrypted`: Encrypted data (nonce prefixed) (or nil on error).
- `error`: Error message string (or nil on success).

##### crypto.encrypt.chacha20(data: string, key: string, aad: string)

Encrypts data using ChaCha20-Poly1305 (authenticated encryption).

Parameters:

- `data`: Data to encrypt.
- `key`: Encryption key (32 bytes).
- `aad`: (Optional) Additional authenticated data.

Returns:

- `encrypted`: Encrypted data (nonce prefixed) (or nil on error).
- `error`: Error message string (or nil on success).

#### Decryption Functions

##### crypto.decrypt.aes(data: string, key: string, aad: string)

Decrypts data using AES-GCM.

Parameters:

- `data`: Encrypted data (with nonce prefixed).
- `key`: Decryption key (16, 24, or 32 bytes).
- `aad`: (Optional) Additional authenticated data.

Returns:

- `decrypted`: Decrypted data (or nil on error).
- `error`: Error message string (or nil on success).

##### crypto.decrypt.chacha20(data: string, key: string, aad: string)

Decrypts data using ChaCha20-Poly1305.

Parameters:

- `data`: Encrypted data (with nonce prefixed).
- `key`: Decryption key (32 bytes).
- `aad`: (Optional) Additional authenticated data.

Returns:

- `decrypted`: Decrypted data (or nil on error).
- `error`: Error message string (or nil on success).

#### JWT Functions

##### crypto.jwt.encode(payload: table, key: string, alg: string)

Creates and signs a JWT.

Parameters:

- `payload`: JWT claims as a table.
- `key`: Signing key.
- `alg`: (Optional) Algorithm ('HS256', 'HS384', 'HS512', default: 'HS256').

Returns:

- `token`: JWT token (or nil on error).
- `error`: Error message string (or nil on success).

##### crypto.jwt.verify(token: string, key: string, alg: string)

Verifies and decodes a JWT.

Parameters:

- `token`: JWT to verify.
- `key`: Verification key.
- `alg`: (Optional) Expected algorithm (default: 'HS256').

Returns:

- `payload`: JWT payload as a table (or nil on error).
- `error`: Error message string (or nil on success).

#### Utility Functions

##### crypto.constant_time_compare(a: string, b: string)

Compares two strings in constant time to prevent timing attacks.

Parameters:

- `a`: First string.
- `b`: Second string.

Returns:

- `equal`: Boolean indicating if strings are equal.

##### crypto.pbkdf2(password: string, salt: string, iterations: number, key_length: number, hash_func: string)

Derives a key using PBKDF2.

Parameters:

- `password`: Base password/key.
- `salt`: Salt value.
- `iterations`: Number of iterations (recommend ≥ 10000).
- `key_length`: Desired key length in bytes.
- `hash_func`: (Optional) Hash function to use ('sha256', 'sha512', default: 'sha256').

Returns:

- `key`: Derived key (or nil on error).
- `error`: Error message string (or nil on success).

## Error Handling

The module returns errors in the following cases:

1. **Invalid Input Type:** If inputs are not of the expected type.

```lua
local bytes, err = crypto.random.bytes("ten") -- bytes: nil, err: "number expected for length"
```

2. **Invalid Parameters:** If function parameters don't meet requirements.

```lua
local encrypted, err = crypto.encrypt.aes("data", "short_key") -- encrypted: nil, err: "key must be 16, 24, or 32 bytes"
```

3. **Operation Failures:** If cryptographic operations fail.

```lua
local verified, err = crypto.jwt.verify("invalid.token", "key") -- verified: nil, err: specific error message
```

## Behavior

1. **Random Data Generation**
    - Functions generate cryptographically secure random data.
    - Empty or negative lengths result in errors.

2. **Encryption/Decryption**
    - Functions validate key lengths and parameter types.
    - Nonces are automatically generated and prefixed to encrypted data.
    - Encrypted data format: `<nonce><tag><ciphertext>`.

3. **JWT Handling**
    - `jwt.encode` validates the payload and signs with the specified algorithm.
    - `jwt.verify` validates the token signature and returns the payload.

## Thread Safety

- The `crypto` module is thread-safe.
- It does not maintain any internal state affected by concurrent access.

## Best Practices

1. **Always check for errors:** Validate return values to handle potential errors.
2. **Use strong keys:** Use appropriate key lengths (AES: 16/24/32 bytes, ChaCha20: 32 bytes).
3. **Validate JWT expiration:** Check expiration claims manually after verification.
4. **Secure random data:** Use `random.bytes` or `random.string` for security-sensitive values.
5. **Constant-time comparison:** Use `constant_time_compare` for comparing sensitive strings.

## Example Usage

```lua
local crypto = require("crypto")

-- Generate a secure state parameter for OAuth
local state, err = crypto.random.string(32)
if err then
  print("Error:", err)
else
  print("State parameter:", state)
end

-- Encrypt an OAuth token for storage
local token = "gho_REDACTED_TOKEN"
local key, err = crypto.pbkdf2("master_secret", "oauth-token-salt", 10000, 32)
if err then
  print("PBKDF2 error:", err)
else
  local encrypted, err = crypto.encrypt.aes(token, key)
  if err then
    print("Encryption error:", err)
  else
    -- Later, decrypt the token
    local decrypted, err = crypto.decrypt.aes(encrypted, key)
    print("Decrypted token:", decrypted)
  end
end

-- Generate HMAC for API request signing
local signature, err = crypto.hmac.sha256("api_secret_key", "request_data")
if err then
  print("HMAC error:", err)
else
  print("Request signature:", signature)
end

-- Create and verify a JWT
local payload = {
  sub = "user123",
  exp = os.time() + 3600
}
local jwt, err = crypto.jwt.encode(payload, "secret_key")
if err then
  print("JWT encode error:", err)
else
  local verified, err = crypto.jwt.verify(jwt, "secret_key")
  if err then
    print("JWT verification error:", err)
  else
    print("JWT verified, subject:", verified.sub)
  end
end

-- PKCE for OAuth
local verifier, err = crypto.random.string(64)
if err then
  print("Error:", err)
else
  local verifier_hash = require("hash").sha256(verifier)
  -- URL-safe encoding would be done with the base64 module
  print("Code verifier:", verifier)
  print("Code challenge:", verifier_hash)
end
```

This specification provides a comprehensive reference for the `crypto` module, detailing its functionality, behavior,
and usage patterns for implementing secure OAuth 2.0 flows and other cryptographic operations.