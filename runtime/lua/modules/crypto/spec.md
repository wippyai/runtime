<!-- SPDX-License-Identifier: MPL-2.0 -->

# crypto

Cryptographic operations including random generation, HMAC, encryption/decryption, JWT handling, and key derivation. Security, nondeterministic.

## Loading

```lua
local crypto = require("crypto")
```

## Functions

### crypto.random.bytes(length: integer) → string, error

Generates cryptographically secure random bytes.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| length | integer | yes | - | Number of bytes (1 to 1,048,576) |

**Returns:** `string, error` - random bytes as string or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| length <= 0 | errors.INVALID | no |
| length > 1MB | errors.INVALID | no |
| random generation fails | errors.INTERNAL | no |

### crypto.random.string(length: integer, charset?: string) → string, error

Generates random string from character set.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| length | integer | yes | - | String length (1 to 1,048,576) |
| charset | string | no | alphanumeric | Characters to use |

**Returns:** `string, error` - random string or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| length <= 0 | errors.INVALID | no |
| length > 1MB | errors.INVALID | no |
| charset empty | errors.INVALID | no |
| random generation fails | errors.INTERNAL | no |

### crypto.random.uuid() → string, error

Generates UUID v4.

**Returns:** `string, error` - UUID string (36 chars) or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| UUID generation fails | errors.INTERNAL | no |

### crypto.hmac.sha256(key: string, data: string) → string, error

Computes HMAC-SHA256 digest.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | HMAC key (cannot be empty) |
| data | string | yes | - | Data to authenticate |

**Returns:** `string, error` - hex-encoded digest (64 chars) or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| key empty | errors.INVALID | no |
| HMAC computation fails | errors.INTERNAL | no |

### crypto.hmac.sha512(key: string, data: string) → string, error

Computes HMAC-SHA512 digest.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | HMAC key (cannot be empty) |
| data | string | yes | - | Data to authenticate |

**Returns:** `string, error` - hex-encoded digest (128 chars) or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| key empty | errors.INVALID | no |
| HMAC computation fails | errors.INTERNAL | no |

### crypto.encrypt.aes(data: string, key: string, aad?: string) → string, error

Encrypts data using AES-GCM authenticated encryption. Nonce is randomly generated and prepended to ciphertext.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Plaintext to encrypt |
| key | string | yes | - | Encryption key (16, 24, or 32 bytes) |
| aad | string | no | nil | Additional authenticated data |

**Returns:** `string, error` - encrypted data (nonce+ciphertext+tag) or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| key not 16/24/32 bytes | errors.INVALID | no |
| cipher creation fails | errors.INTERNAL | no |
| nonce generation fails | errors.INTERNAL | no |

### crypto.encrypt.chacha20(data: string, key: string, aad?: string) → string, error

Encrypts data using ChaCha20-Poly1305 authenticated encryption. Nonce is randomly generated and prepended to ciphertext.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Plaintext to encrypt |
| key | string | yes | - | Encryption key (must be 32 bytes) |
| aad | string | no | nil | Additional authenticated data |

**Returns:** `string, error` - encrypted data (nonce+ciphertext+tag) or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| key not 32 bytes | errors.INVALID | no |
| cipher creation fails | errors.INTERNAL | no |
| nonce generation fails | errors.INTERNAL | no |

### crypto.decrypt.aes(data: string, key: string, aad?: string) → string, error

Decrypts AES-GCM encrypted data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Encrypted data (from encrypt.aes) |
| key | string | yes | - | Decryption key (16, 24, or 32 bytes) |
| aad | string | no | nil | Additional authenticated data (must match encryption) |

**Returns:** `string, error` - plaintext or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| key not 16/24/32 bytes | errors.INVALID | no |
| data too short | errors.INVALID | no |
| cipher creation fails | errors.INTERNAL | no |
| decryption fails | errors.INTERNAL | no |
| authentication fails | errors.INTERNAL | no |

### crypto.decrypt.chacha20(data: string, key: string, aad?: string) → string, error

Decrypts ChaCha20-Poly1305 encrypted data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Encrypted data (from encrypt.chacha20) |
| key | string | yes | - | Decryption key (must be 32 bytes) |
| aad | string | no | nil | Additional authenticated data (must match encryption) |

**Returns:** `string, error` - plaintext or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| key not 32 bytes | errors.INVALID | no |
| data too short | errors.INVALID | no |
| cipher creation fails | errors.INTERNAL | no |
| decryption fails | errors.INTERNAL | no |
| authentication fails | errors.INTERNAL | no |

### crypto.jwt.encode(payload: table, key: string, alg?: string) → string, error

Creates and signs JWT token.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| payload | table | yes | - | JWT claims |
| key | string | yes | - | Signing key (secret for HMAC, PEM private key for RS256) |
| alg | string | no | "HS256" | Algorithm: "HS256", "HS384", "HS512", "RS256" |

**payload fields:**
- Any claim fields (sub, iss, exp, iat, etc.)
- `_header`: optional table for custom JWT header fields (e.g., {kid = "key-id"})

**Returns:** `string, error` - JWT token string or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| payload not table | errors.INVALID | no |
| invalid RSA private key (RS256) | errors.INVALID | no |
| token signing fails | errors.INTERNAL | no |

### crypto.jwt.verify(token: string, key: string, alg?: string, require_exp?: boolean) → table, error

Verifies and decodes JWT token.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| token | string | yes | - | JWT token to verify |
| key | string | yes | - | Verification key (secret for HMAC, PEM public key for RS256) |
| alg | string | no | "HS256" | Expected algorithm: "HS256", "HS384", "HS512", "RS256" |
| require_exp | boolean | no | true | Require exp claim and validate expiration |

**Returns:** `table, error` - JWT claims as table or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| token format invalid | errors.INTERNAL | no |
| signature invalid | errors.INTERNAL | no |
| algorithm mismatch | errors.INTERNAL | no |
| token expired | errors.INTERNAL | no |
| missing exp (when require_exp=true) | errors.INTERNAL | no |
| invalid RSA public key (RS256) | errors.INTERNAL | no |

### crypto.pbkdf2(password: string, salt: string, iterations: integer, key_length: integer, hash?: string) → string, error

Derives key using PBKDF2 key derivation function.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| password | string | yes | - | Password/passphrase (cannot be empty) |
| salt | string | yes | - | Salt value (cannot be empty) |
| iterations | integer | yes | - | Iteration count (1 to 10,000,000) |
| key_length | integer | yes | - | Desired key length in bytes |
| hash | string | no | "sha256" | Hash function: "sha256" or "sha512" |

**Returns:** `string, error` - derived key as bytes or nil + structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| password empty | errors.INVALID | no |
| salt empty | errors.INVALID | no |
| iterations <= 0 | errors.INVALID | no |
| iterations > 10,000,000 | errors.INVALID | no |
| key_length <= 0 | errors.INVALID | no |
| unsupported hash function | errors.INVALID | no |

### crypto.constant_time_compare(a: string, b: string) → boolean

Compares two strings in constant time to prevent timing attacks.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| a | string | yes | - | First string |
| b | string | yes | - | Second string |

**Returns:** `boolean` - true if strings are equal, false otherwise

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = crypto.random.bytes(16)
if err then
    if err:kind() == errors.INVALID then
        -- invalid input
    elseif err:kind() == errors.INTERNAL then
        -- internal crypto error
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local crypto = require("crypto")

-- Generate random state parameter
local state, err = crypto.random.string(32)
if err then error(err) end

-- HMAC signature
local sig, err = crypto.hmac.sha256("api_secret", "request_data")
if err then error(err) end

-- Encrypt sensitive data
local key = string.rep("a", 32)
local encrypted, err = crypto.encrypt.aes("secret data", key)
if err then error(err) end

local decrypted, err = crypto.decrypt.aes(encrypted, key)
if err then error(err) end
-- decrypted == "secret data"

-- JWT with HMAC
local payload = {
    sub = "user123",
    exp = os.time() + 3600,
    _header = { kid = "key-id" }
}
local token, err = crypto.jwt.encode(payload, "secret")
if err then error(err) end

local claims, err = crypto.jwt.verify(token, "secret")
if err then error(err) end
-- claims.sub == "user123"

-- PBKDF2 key derivation
local derived, err = crypto.pbkdf2("password", "salt", 10000, 32, "sha256")
if err then error(err) end

-- Constant-time comparison
local equal = crypto.constant_time_compare("hash1", "hash2")
```
