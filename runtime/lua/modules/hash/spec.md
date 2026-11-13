<!-- SPDX-License-Identifier: MPL-2.0 -->

# hash

Cryptographic hash functions, HMAC, PBKDF2, and MinHash sketches for near-duplicate
detection. Encoding, security, deterministic.

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

Computes FNV-1 32-bit hash of input data.

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

### fnv64(data: string, raw?: boolean) → string, error

Computes FNV-1 64-bit hash of input data. The result is a string because a 64-bit value does not fit a Lua number without losing precision.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to hash |
| raw | boolean | no | false | Return 8 big-endian bytes instead of 16 hex characters |

**Returns:**
- Success: string (16 hex characters, or 8 bytes if raw)
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

### pbkdf2(password: string, salt: string, iterations: integer, key_length: integer, algo?: string) → string, error

Derives a key using PBKDF2-HMAC.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| password | string | yes | - | Password or secret input |
| salt | string | yes | - | Salt bytes |
| iterations | integer | yes | - | Positive iteration count |
| key_length | integer | yes | - | Positive output length in bytes |
| algo | string | no | "sha256" | Hash function: "sha256" or "sha512" |

**Returns:**
- Success: raw derived key bytes
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| password not a string or empty | errors.INVALID | no |
| salt not a string or empty | errors.INVALID | no |
| iterations not positive or above maximum | errors.INVALID | no |
| key_length not positive | errors.INVALID | no |
| algo unsupported | errors.INVALID | no |

## minhash

`hash.minhash` computes one-permutation MinHash sketches with deterministic probe densification over
token shingles, their LSH band keys and Jaccard estimates. Every output is bit-exact across
platforms and implementations of the algorithm below; no value passes through a float
except the similarity.

### Algorithm

Options (all functions refuse unknown options):

| Option | Default | Meaning |
|--------|---------|---------|
| k | 128 | sketch bins, 1..65536, coprime with 55 (not a multiple of 5 or 11) |
| shingle | 5 | tokens per shingle, 1..1024 |
| bands | 16 | LSH bands; bands * rows must equal the sketch bins |
| rows | 8 | bins per band |
| domain | "minhash.lsh.v1" | namespace string framed into every band key |

Fixed constants: M = 2147483647 (2^31-1), P = 1000003, C = 2654435761, stride = 55.

1. **Tokens.** An input is a string or an array of token strings. A string is split by the
   default tokenizer: a token is a maximal run of word bytes - ASCII `[A-Za-z0-9_]` and
   every byte `>= 0x80` - of any length; every other byte is a separator. ASCII letters are
   lowered; other bytes keep their value. An array supplies the tokens as given, each a
   non-empty string, unchanged.
2. **Normalized digest.** `normalized` is the hex sha256 of the tokens joined by one space
   (sha256 of the empty string when there are no tokens).
3. **Shingles.** A token hashes to its 32-bit FNV-1 hash (offset `0x811C9DC5`, prime
   `0x01000193`, multiply then xor) modulo M. A shingle is `shingle` consecutive tokens, or
   one shingle of all tokens when there are fewer; its hash is
   `((t1*P + t2)*P + ... + tn) mod M`.
4. **Sketch.** Bin `s mod k` keeps the least full shingle hash `s`. The full hash preserves
   its source-bin identity after densification. An empty
   bin `i` (0-based) takes the value the first non-empty bin of the probe sequence
   `j(a) = (((i+1)*C mod M) + stride*a) mod k`, `a = 1..k`, held before densification.
   Input without tokens has no sketch.
5. **Bands.** Band `b` (0-based) covers bins `rows*b .. rows*b+rows-1`. Its key is the
   first 8 bytes, as 16 lowercase hex digits, of
   `sha256(frame(domain, decimal(b), the band's bins as 32-bit big-endian words))`, where
   `frame` concatenates each field prefixed by its decimal byte length and `:`.
6. **Similarity.** The Jaccard similarity of two inputs' shingle sets is estimated by the
   share of equal bins of their sketches.

A sketch crosses the Lua boundary as `4*k` bytes: its bins as consecutive 32-bit big-endian
words, with at most 65536 bins. Compare sketches made with the same tokenizer, shingle
width and algorithm; the serialized words do not carry those parameters.

### minhash.signature(input: string | {string}, options?: MinHashOptions) → MinHashSignature, error

Computes everything an input is compared by in one pass.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| input | string or array of strings | yes | - | Text, or tokens |
| options | table | no | - | `k`, `shingle`, `bands`, `rows`, `domain` |

**Returns:**
- Success: table
  - `tokens: integer` - token count
  - `normalized: string` - hex sha256 of the tokens joined by one space
  - `sketch: string?` - `4*k` bytes; absent when there are no tokens
  - `bands: {string}?` - band keys in band order; absent when there are no tokens
- Error: `nil, error` - structured error

### minhash.sketch(input: string | {string}, options?: MinHashSketchOptions) → string, error

Computes the sketch of an input.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| input | string or array of strings | yes | - | Text, or tokens |
| options | table | no | - | `k`, `shingle` |

**Returns:**
- Success: `4*k` bytes, or `""` when there are no tokens
- Error: `nil, error` - structured error

### minhash.bands(sketch: string, options?: MinHashBandOptions) → {string}, error

Computes the LSH band keys of a sketch.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sketch | string | yes | - | Sketch bytes |
| options | table | no | - | `bands`, `rows`, `domain`; bands * rows must equal the sketch's bins |

**Returns:**
- Success: array of 16-digit hex keys
- Error: `nil, error` - structured error

### minhash.similarity(a: string, b: string) → number, error

Estimates the Jaccard similarity of the inputs two sketches were computed from: equal bins
divided by bins.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| a | string | yes | - | Sketch bytes |
| b | string | yes | - | Sketch bytes of the same size |

**Returns:**
- Success: number in 0..1
- Error: `nil, error` - structured error

### minhash.new(options?: MinHashOptions) → MinHasher, error

Creates a streaming MinHasher for text too large to hold in one string. Chunks are
tokenized by the default tokenizer; a token may span chunks. Memory stays bounded: the
hasher holds the hash of the token in progress, the last `shingle` token hashes, the `k`
bins and the running normalized digest.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | no | - | `k`, `shingle`, `bands`, `rows`, `domain` |

**MinHasher methods:**
- `update(chunk: string) → boolean, error` - appends text; `false, error` when chunk is not a string
- `sum() → MinHashSignature, error` - the signature of everything appended so far, equal to
  `minhash.signature` of the concatenation; the state is kept
- `reset()` - discards everything appended so far

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| input neither a string nor an array of non-empty strings | errors.INVALID | no |
| sketch not a string, empty, or not a whole number of 32-bit words | errors.INVALID | no |
| options not a table, unknown option, option of the wrong type | errors.INVALID | no |
| k or shingle out of range, k not coprime with 55 | errors.INVALID | no |
| bands * rows not equal to the bins | errors.INVALID | no |
| sketches of different sizes | errors.INVALID | no |
| update chunk not a string | errors.INVALID | no |

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

-- MinHash: near-duplicate candidates share band keys; similarity confirms them
local a = hash.minhash.signature("local function fold(state, event) return apply(state, event) end")
local b = hash.minhash.signature("local function fold(state, event) return apply(state, other) end")
assert(a.tokens == 10 and #a.sketch == 512 and #a.bands == 16)
local similarity = hash.minhash.similarity(a.sketch, b.sketch)
assert(similarity > 0.3 and similarity < 1)

-- Case and separators do not change the tokens; an array of tokens is used as given
assert(hash.minhash.sketch("LOCAL function   FOLD; state event return apply state event end") == a.sketch)
assert(hash.minhash.sketch({ "local", "function", "fold", "state", "event", "return", "apply", "state", "event", "end" }) == a.sketch)

-- Streaming: chunks may split tokens
local m = hash.minhash.new()
m:update("local function fold(state, ev")
m:update("ent) return apply(state, event) end")
assert(m:sum().sketch == a.sketch)
```
