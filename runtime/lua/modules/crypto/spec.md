# Lua Crypto Module Design Specification

Based on your requirements, I'll design a comprehensive crypto module that integrates well with your existing Lua/Go system for OAuth 2.0 implementation.

## Module Structure

The crypto module will follow a hierarchical structure similar to your existing hash module:

```lua
crypto = {
    random = { ... },    -- Secure random generation functions
    encode = { ... },    -- Encoding functions
    decode = { ... },    -- Decoding functions
    encrypt = { ... },   -- Encryption functions
    decrypt = { ... },   -- Decryption functions
    hmac = { ... },      -- HMAC functions
    jwt = { ... },       -- JWT handling functions
    utils = { ... }      -- Utility functions
}
```

## API Specification

### Random Data Generation

```lua
-- Generates cryptographically secure random bytes
-- @param length (number): Number of random bytes to generate
-- @return (string) Random bytes or (nil, error_message) on failure
crypto.random.bytes(length)

-- Generates a random UUID (v4)
-- @return (string) UUID string or (nil, error_message) on failure
crypto.random.uuid()

-- Generates a random string using a specified character set
-- @param length (number): Length of the string
-- @param charset (string, optional): Characters to use (default: alphanumeric)
-- @return (string) Random string or (nil, error_message) on failure
crypto.random.string(length, charset)
```

### Encoding/Decoding

```lua
-- Encodes data using standard Base64 encoding
-- @param data (string): Data to encode
-- @return (string) Base64-encoded data or (nil, error_message) on failure
crypto.encode.base64(data)

-- Encodes data using URL-safe Base64 encoding (no padding)
-- @param data (string): Data to encode
-- @return (string) Base64URL-encoded data or (nil, error_message) on failure
crypto.encode.base64url(data)

-- Encodes binary data as hexadecimal string
-- @param data (string): Data to encode
-- @return (string) Hex-encoded data or (nil, error_message) on failure
crypto.encode.hex(data)

-- Corresponding decode functions
crypto.decode.base64(data)
crypto.decode.base64url(data)
crypto.decode.hex(data)
```

### Symmetric Encryption/Decryption

```lua
-- Encrypts data using AES-GCM (authenticated encryption)
-- @param data (string): Data to encrypt
-- @param key (string): Encryption key (16, 24, or 32 bytes)
-- @param aad (string, optional): Additional authenticated data
-- @return (string) Encrypted data (nonce prefixed) or (nil, error_message) on failure
crypto.encrypt.aes_gcm(data, key, aad)

-- Decrypts data using AES-GCM
-- @param data (string): Encrypted data (with nonce prefixed)
-- @param key (string): Decryption key (16, 24, or 32 bytes)
-- @param aad (string, optional): Additional authenticated data (must match encryption)
-- @return (string) Decrypted data or (nil, error_message) on failure
crypto.decrypt.aes_gcm(data, key, aad)

-- Encrypts data using ChaCha20-Poly1305 (authenticated encryption)
-- @param data (string): Data to encrypt
-- @param key (string): Encryption key (32 bytes)
-- @param aad (string, optional): Additional authenticated data
-- @return (string) Encrypted data (nonce prefixed) or (nil, error_message) on failure
crypto.encrypt.chacha20poly1305(data, key, aad)

-- Decrypts data using ChaCha20-Poly1305
-- @param data (string): Encrypted data (with nonce prefixed)
-- @param key (string): Decryption key (32 bytes)
-- @param aad (string, optional): Additional authenticated data (must match encryption)
-- @return (string) Decrypted data or (nil, error_message) on failure
crypto.decrypt.chacha20poly1305(data, key, aad)
```

### HMAC Functions

```lua
-- Calculates HMAC-SHA256
-- @param key (string): HMAC key
-- @param data (string): Data to authenticate
-- @return (string) HMAC digest or (nil, error_message) on failure
crypto.hmac.sha256(key, data)

-- Calculates HMAC-SHA512
-- @param key (string): HMAC key
-- @param data (string): Data to authenticate
-- @return (string) HMAC digest or (nil, error_message) on failure
crypto.hmac.sha512(key, data)
```

### JWT Handling

```lua
-- Creates and signs a JWT
-- @param payload (table): JWT claims
-- @param key (string): Signing key
-- @param alg (string, optional): Algorithm to use ('HS256', 'HS384', 'HS512', default: 'HS256')
-- @return (string) JWT token or (nil, error_message) on failure
crypto.jwt.encode(payload, key, alg)

-- Verifies and decodes a JWT
-- @param token (string): JWT to verify
-- @param key (string): Verification key
-- @param alg (string, optional): Expected algorithm ('HS256', 'HS384', 'HS512', default: 'HS256')
-- @return (table) JWT payload or (nil, error_message) on failure
crypto.jwt.verify(token, key, alg)
```

### Utility Functions

```lua
-- Compares two strings in constant time to prevent timing attacks
-- @param a (string): First string
-- @param b (string): Second string
-- @return (boolean) True if strings are equal, false otherwise
crypto.utils.constant_time_compare(a, b)

-- Derives a key using PBKDF2
-- @param password (string): Base password/key
-- @param salt (string): Salt value
-- @param iterations (number): Number of iterations (recommend ≥ 10000)
-- @param key_length (number): Desired key length in bytes
-- @param hash_func (string, optional): Hash function to use ('sha256', 'sha512', default: 'sha256')
-- @return (string) Derived key or (nil, error_message) on failure
crypto.utils.pbkdf2(password, salt, iterations, key_length, hash_func)
```

## Implementation Approach

### Go Implementation

Here's a sample implementation of the `random.bytes` function following your pattern:

```go
func (m *Crypto) randomBytes(l *lua.LState) int {
    // Validate parameter
    length := l.CheckInt(1)
    if length <= 0 {
        l.ArgError(1, "length must be a positive integer")
        return 0
    }
    
    // Create a buffer to hold the random bytes
    buf := make([]byte, length)
    
    // Generate random bytes using crypto/rand
    _, err := rand.Read(buf)
    if err != nil {
        l.Push(lua.LNil)
        l.Push(lua.LString(err.Error()))
        return 2
    }
    
    // Return the random bytes as a string
    l.Push(lua.LString(string(buf)))
    return 1
}
```

And here's the `encrypt.aes_gcm` function:

```go
func (m *Crypto) encryptAESGCM(l *lua.LState) int {
    // Validate parameters
    if l.Get(1).Type() != lua.LTString || l.Get(2).Type() != lua.LTString {
        l.ArgError(1, "string parameters expected")
        return 0
    }
    
    plaintext := []byte(l.ToString(1))
    key := []byte(l.ToString(2))
    
    // Get optional additional authenticated data
    var aad []byte
    if l.GetTop() >= 3 && l.Get(3).Type() == lua.LTString {
        aad = []byte(l.ToString(3))
    }
    
    // Validate key length
    switch len(key) {
    case 16, 24, 32: // AES-128, AES-192, AES-256
        // Valid key length
    default:
        l.Push(lua.LNil)
        l.Push(lua.LString("key must be 16, 24, or 32 bytes"))
        return 2
    }
    
    // Create a new AES cipher
    block, err := aes.NewCipher(key)
    if err != nil {
        l.Push(lua.LNil)
        l.Push(lua.LString(err.Error()))
        return 2
    }
    
    // Create a new GCM AEAD
    aesGCM, err := cipher.NewGCM(block)
    if err != nil {
        l.Push(lua.LNil)
        l.Push(lua.LString(err.Error()))
        return 2
    }
    
    // Create a nonce
    nonce := make([]byte, aesGCM.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        l.Push(lua.LNil)
        l.Push(lua.LString(err.Error()))
        return 2
    }
    
    // Encrypt and authenticate
    ciphertext := aesGCM.Seal(nonce, nonce, plaintext, aad)
    
    // Return the encrypted data (nonce + ciphertext)
    l.Push(lua.LString(ciphertext))
    return 1
}
```

### Module Registration

Here's how the module registration would look:

```go
type Crypto struct {
    // Internal state if needed
}

func NewCrypto() *Crypto {
    return &Crypto{}
}

func (c *Crypto) Loader(L *lua.LState) int {
    // Create module table
    mod := L.NewTable()
    
    // Register random submodule
    randomMod := L.NewTable()
    L.SetField(randomMod, "bytes", L.NewFunction(c.randomBytes))
    L.SetField(randomMod, "uuid", L.NewFunction(c.randomUUID))
    L.SetField(randomMod, "string", L.NewFunction(c.randomString))
    L.SetField(mod, "random", randomMod)
    
    // Register the rest of the submodules and functions...
    
    // Return the module
    L.Push(mod)
    return 1
}
```

## Addressing Your Specific Questions

### 1. What cryptographic algorithms should we support for symmetric encryption?

I recommend implementing:

- **AES-GCM**: Modern authenticated encryption that provides both confidentiality and integrity. This should be your primary choice.
- **ChaCha20-Poly1305**: A high-performance alternative to AES-GCM that works well in software implementations.

Both algorithms provide authenticated encryption with associated data (AEAD), which is crucial for securing OAuth tokens.

### 2. How should we handle key management for encryption/decryption?

For OAuth implementation, I recommend:

1. **Use a master key approach**:
    - Store a single master key securely (environment variable or secure storage).
    - Use key derivation to generate purpose-specific keys.

2. **Implement key derivation**:
   ```lua
   -- Example usage
   local token_key = crypto.utils.pbkdf2(master_key, "oauth-token-encryption", 10000, 32)
   ```

3. **Store version/purpose info with encrypted data**:
   ```lua
   -- Format: v1:purpose:encrypted_data
   local store_data = "v1:oauth:" .. crypto.encrypt.aes_gcm(token, key)
   ```

This approach allows for future key rotation while maintaining compatibility with existing encrypted data.

### 3. What's the best way to generate cryptographically secure random data in Go that's exposed to Lua?

Use Go's `crypto/rand` package directly, as shown in the `randomBytes` implementation above. This approach:

- Uses Go's secure random source (`crypto/rand` not `math/rand`)
- Properly handles errors from the random source
- Returns the random data as a Lua string

### 4. What additional security features should be considered for a production OAuth implementation?

For a production-ready OAuth implementation:

1. **CSRF Protection**:
   ```lua
   -- Generate and store a random state parameter
   local state = crypto.random.string(32)
   session.state = state
   ```

2. **PKCE (Proof Key for Code Exchange)**:
   ```lua
   -- Generate code verifier and challenge
   local verifier = crypto.random.string(64)
   local challenge = crypto.encode.base64url(crypto.hash.sha256(verifier))
   ```

3. **Secure Token Storage**:
   ```lua
   -- Encrypt tokens before storing
   local encrypted = crypto.encrypt.aes_gcm(token, key)
   ```

4. **Token Validation**:
   ```lua
   -- When validating tokens, use constant-time comparison
   if not crypto.utils.constant_time_compare(stored_token, received_token) then
       -- Token invalid
   end
   ```

5. **Token Expiration Handling**: Implement proper handling of token expiration and refreshing.

### 5. How should we structure the module API for consistency with our existing modules?

Based on your hash module example:

1. **Function Signature**: Follow the pattern of returning `(result, nil)` on success and `(nil, error_message)` on failure.
2. **Error Handling**: Use `l.ArgError()` for parameter validation and return `(nil, error_message)` for runtime errors.
3. **Naming Convention**: Use snake_case for function names and clear descriptive names.
4. **Submodule Organization**: Group related functions into logical submodules.

## Usage Examples

```lua
-- Generate a secure state parameter for OAuth
local state = crypto.random.string(32)
print("State parameter:", crypto.encode.base64url(state))

-- Encrypt an OAuth token for storage
local token = "gho_REDACTED_TOKEN"
local key = crypto.utils.pbkdf2(master_key, "oauth-token", 10000, 32)
local encrypted = crypto.encrypt.aes_gcm(token, key)

-- Store the encrypted token
db.save("user_token", crypto.encode.base64(encrypted))

-- Later, retrieve and decrypt
local encrypted = crypto.decode.base64(db.get("user_token"))
local token = crypto.decrypt.aes_gcm(encrypted, key)

-- Create a JWT
local payload = {
    sub = "user123",
    exp = os.time() + 3600
}
local jwt = crypto.jwt.encode(payload, signing_key, "HS256")
```

## Go Dependencies

To implement this module, you'll need these Go packages:

```go
import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "crypto/hmac"
    "crypto/sha256"
    "crypto/sha512"
    "crypto/subtle"
    "encoding/base64"
    "encoding/hex"
    "github.com/golang-jwt/jwt/v5"
    "github.com/google/uuid"
    "golang.org/x/crypto/chacha20poly1305"
    "golang.org/x/crypto/pbkdf2"
)
```

This design provides you with a robust crypto module that satisfies all your requirements while maintaining good security practices and consistency with your existing code base.
You're right! If you prefer the nested structure with submodules like `.random`, we can absolutely go with that approach. While your existing modules use a flat structure, the crypto module encompasses more diverse functionality that benefits from logical grouping.

Here's the revised structure keeping the submodule organization:

```lua
crypto = {
    random = {
        bytes = function(length) ... end,
        string = function(length, charset) ... end,
        uuid = function() ... end
    },
    
    encrypt = {
        aes = function(data, key, aad) ... end,
        chacha20 = function(data, key, aad) ... end
    },
    
    decrypt = {
        aes = function(data, key, aad) ... end,
        chacha20 = function(data, key, aad) ... end
    },
    
    hmac = {
        sha256 = function(key, data) ... end,
        sha512 = function(key, data) ... end
    },
    
    jwt = {
        encode = function(payload, key, alg) ... end,
        verify = function(token, key, alg) ... end
    },
    
    -- Utility functions at the top level
    url_encode = function(str) ... end,
    url_decode = function(str) ... end,
    constant_time_compare = function(a, b) ... end,
    pbkdf2 = function(password, salt, iterations, key_length, hash_func) ... end
}
```

This hybrid approach:
1. Groups related functions into logical submodules (random, encrypt, decrypt, hmac, jwt)
2. Keeps general utility functions at the top level
3. Makes the API more discoverable and organized
4. Provides a cleaner separation of concerns

The crypto module is more complex than your existing modules, so this hierarchical organization can make it easier to work with. This approach also allows for future expansion by adding new functions to the appropriate submodules without cluttering the main namespace.

You're right - I should have checked your existing modules more carefully. Let me revise my design to be more consistent with your existing libraries.

## Lua Crypto Module Specification (Revised)

### Overview

The `crypto` module provides cryptographic functions to support OAuth 2.0 authentication, secure random data generation, and encryption/decryption operations. It works alongside your existing `hash` and `base64` modules without duplication.

### Module Interface

```lua
local crypto = require("crypto")
```

### Global Functions

#### `crypto.random_bytes(length: number)`

Generates cryptographically secure random bytes.

Parameters:
- `length`: Number of random bytes to generate.

Returns:
- `bytes`: Random bytes as a string (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.random_string(length: number, charset: string)`

Generates a random string using the specified character set.

Parameters:
- `length`: Length of the string to generate.
- `charset`: (Optional) Characters to use (default: alphanumeric).

Returns:
- `str`: Random string (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.random_uuid()`

Generates a random UUID (v4).

Returns:
- `uuid`: UUID string (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.url_encode(str: string)`

Encodes a string for URL-safe use (RFC 4648 base64url).

Parameters:
- `str`: String to encode.

Returns:
- `encoded`: URL-safe encoded string (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.url_decode(str: string)`

Decodes a URL-safe encoded string.

Parameters:
- `str`: URL-safe encoded string to decode.

Returns:
- `decoded`: Decoded string (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.hmac_sha256(key: string, data: string)`

Calculates HMAC-SHA256.

Parameters:
- `key`: HMAC key.
- `data`: Data to authenticate.

Returns:
- `digest`: Hex-encoded HMAC digest (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.hmac_sha512(key: string, data: string)`

Calculates HMAC-SHA512.

Parameters:
- `key`: HMAC key.
- `data`: Data to authenticate.

Returns:
- `digest`: Hex-encoded HMAC digest (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.encrypt_aes(data: string, key: string, aad: string)`

Encrypts data using AES-GCM (authenticated encryption).

Parameters:
- `data`: Data to encrypt.
- `key`: Encryption key (16, 24, or 32 bytes).
- `aad`: (Optional) Additional authenticated data.

Returns:
- `encrypted`: Encrypted data (nonce prefixed) (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.decrypt_aes(data: string, key: string, aad: string)`

Decrypts data using AES-GCM.

Parameters:
- `data`: Encrypted data (with nonce prefixed).
- `key`: Decryption key (16, 24, or 32 bytes).
- `aad`: (Optional) Additional authenticated data.

Returns:
- `decrypted`: Decrypted data (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.encrypt_chacha20(data: string, key: string, aad: string)`

Encrypts data using ChaCha20-Poly1305 (authenticated encryption).

Parameters:
- `data`: Data to encrypt.
- `key`: Encryption key (32 bytes).
- `aad`: (Optional) Additional authenticated data.

Returns:
- `encrypted`: Encrypted data (nonce prefixed) (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.decrypt_chacha20(data: string, key: string, aad: string)`

Decrypts data using ChaCha20-Poly1305.

Parameters:
- `data`: Encrypted data (with nonce prefixed).
- `key`: Decryption key (32 bytes).
- `aad`: (Optional) Additional authenticated data.

Returns:
- `decrypted`: Decrypted data (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.constant_time_compare(a: string, b: string)`

Compares two strings in constant time to prevent timing attacks.

Parameters:
- `a`: First string.
- `b`: Second string.

Returns:
- `equal`: Boolean indicating if strings are equal.

#### `crypto.pbkdf2(password: string, salt: string, iterations: number, key_length: number, hash_func: string)`

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

#### `crypto.jwt_encode(payload: table, key: string, alg: string)`

Creates and signs a JWT.

Parameters:
- `payload`: JWT claims as a table.
- `key`: Signing key.
- `alg`: (Optional) Algorithm ('HS256', 'HS384', 'HS512', default: 'HS256').

Returns:
- `token`: JWT token (or nil on error).
- `error`: Error message string (or nil on success).

#### `crypto.jwt_verify(token: string, key: string, alg: string)`

Verifies and decodes a JWT.

Parameters:
- `token`: JWT to verify.
- `key`: Verification key.
- `alg`: (Optional) Expected algorithm (default: 'HS256').

Returns:
- `payload`: JWT payload as a table (or nil on error).
- `error`: Error message string (or nil on success).

### Error Handling

The module returns errors in the following cases:

1. **Invalid Input Type:** If inputs are not of the expected type.

```lua
local bytes, err = crypto.random_bytes("ten") -- bytes: nil, err: "number expected for length"
```

2. **Invalid Parameters:** If function parameters don't meet requirements.

```lua
local encrypted, err = crypto.encrypt_aes("data", "short_key") -- encrypted: nil, err: "key must be 16, 24, or 32 bytes"
```

3. **Operation Failures:** If cryptographic operations fail.

```lua
local verified, err = crypto.jwt_verify("invalid.token", "key") -- verified: nil, err: specific error message
```

### Behavior

1. **Random Data Generation**
    - Functions generate cryptographically secure random data.
    - Empty or negative lengths result in errors.

2. **Encryption/Decryption**
    - Functions validate key lengths and parameter types.
    - Nonces are automatically generated and prefixed to encrypted data.
    - Encrypted data format: `<nonce><tag><ciphertext>`.

3. **JWT Handling**
    - `jwt_encode` validates the payload and signs with the specified algorithm.
    - `jwt_verify` validates the token signature and returns the payload.

4. **URL-Safe Encoding**
    - `url_encode` uses RFC 4648 base64url encoding (URL-safe, no padding).
    - `url_decode` decodes URL-safe base64 strings.

### Thread Safety

- The `crypto` module is thread-safe.
- It does not maintain any internal state affected by concurrent access.

### Best Practices

1. **Always check for errors:** Validate return values to handle potential errors.
2. **Use strong keys:** Use appropriate key lengths (AES: 16/24/32 bytes, ChaCha20: 32 bytes).
3. **Validate JWT expiration:** Check expiration claims manually after verification.
4. **Secure random data:** Use `random_bytes` or `random_string` for security-sensitive values.
5. **Constant-time comparison:** Use `constant_time_compare` for comparing sensitive strings.

### Example Usage

```lua
local crypto = require("crypto")
local base64 = require("base64")

-- Generate a secure state parameter for OAuth
local state, err = crypto.random_string(32)
if err then
  print("Error:", err)
else
  local urlsafe_state, err = crypto.url_encode(state)
  print("State parameter:", urlsafe_state)
end

-- Encrypt an OAuth token for storage
local token = "gho_REDACTED_TOKEN"
local key, err = crypto.pbkdf2("master_secret", "oauth-token-salt", 10000, 32)
if err then
  print("PBKDF2 error:", err)
else
  local encrypted, err = crypto.encrypt_aes(token, key)
  if err then
    print("Encryption error:", err)
  else
    -- Store the encrypted token
    local base64_encrypted = base64.encode(encrypted)
    print("Encrypted token:", base64_encrypted)
  end
end

-- Implement PKCE for OAuth
local verifier, err = crypto.random_string(64)
if err then
  print("Error:", err)
else
  local verifier_hash = hash.sha256(verifier)
  local challenge, err = crypto.url_encode(verifier_hash)
  print("Code challenge:", challenge)
end

-- Create a JWT
local payload = {
  sub = "user123",
  exp = os.time() + 3600
}
local jwt, err = crypto.jwt_encode(payload, "secret_key")
if err then
  print("JWT encode error:", err)
else
  print("JWT:", jwt)
end

-- Verify JWT
local verified, err = crypto.jwt_verify(jwt, "secret_key")
if err then
  print("JWT verification error:", err)
else
  print("JWT verified, subject:", verified.sub)
end
```

## Addressing Your Specific Questions

### 1. What cryptographic algorithms should we support for symmetric encryption?

Based on your existing modules and OAuth requirements:

- **AES-GCM**: Modern, widely supported AEAD cipher
- **ChaCha20-Poly1305**: Alternative that performs well in software implementations

Both provide authenticated encryption, which is crucial for secure token storage.

### 2. How should we handle key management for encryption/decryption?

For a Lua API following your module pattern:

1. **Provide PBKDF2 for key derivation**:
   ```lua
   local key, err = crypto.pbkdf2(master_secret, "oauth-token-salt", 10000, 32)
   ```

2. **Use a master key approach**:
    - Store a single master key securely (environment variable or secure storage)
    - Use PBKDF2 to derive purpose-specific keys

3. **Add metadata to encrypted data**:
   ```lua
   local storage_format = "v1:oauth:" .. base64.encode(encrypted_data)
   ```

### 3. What's the best way to generate cryptographically secure random data in Go for Lua?

Use Go's `crypto/rand` package, similar to your hash module's approach:

- Properly handle errors from the random source
- Validate input parameters in Lua
- Return the random data as a Lua string with error information

### 4. What additional security features should be considered for a production OAuth implementation?

Your OAuth implementation should include:

1. **CSRF Protection**:
   ```lua
   local state, err = crypto.random_string(32)
   ```

2. **PKCE (Proof Key for Code Exchange)**:
   ```lua
   local verifier, err = crypto.random_string(64)
   local challenge = crypto.url_encode(hash.sha256(verifier))
   ```

3. **Secure Token Storage**:
   ```lua
   local encrypted, err = crypto.encrypt_aes(token, key)
   ```

4. **Constant-Time Comparisons**:
   ```lua
   if not crypto.constant_time_compare(stored_token, received_token) then
       -- Token invalid
   end
   ```

5. **JWT Verification** (if using):
   ```lua
   local payload, err = crypto.jwt_verify(token, key)
   ```

### 5. How should we structure the module API for consistency with our existing modules?

Following your existing modules:

1. **Flat Structure**: Direct functions on the module table
2. **Consistent Return Pattern**: `result, error` where error is nil on success
3. **Clear Naming**: Descriptive function names in snake_case
4. **Parameter Validation**: Check types and provide descriptive errors
5. **Documentation**: Clear function descriptions with parameters and return values
6. **Avoid Duplication**: Leverage existing modules where applicable (hash, base64)

This design maintains consistency with your existing modules while adding the crypto functionality you need for OAuth.


