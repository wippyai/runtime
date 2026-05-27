<!-- SPDX-License-Identifier: MPL-2.0 -->

# compress

Data compression and decompression supporting gzip, deflate, zlib, brotli, and zstd. Encoding, deterministic.

## Loading

```lua
local compress = require("compress")
```

## Functions

The module provides five compression algorithms, each with identical `encode` and `decode` functions:

- `compress.gzip` - GZIP compression (RFC 1952)
- `compress.deflate` - DEFLATE compression (RFC 1951)
- `compress.zlib` - zlib compression (RFC 1950)
- `compress.brotli` - Brotli compression (RFC 7932)
- `compress.zstd` - Zstandard compression (RFC 8878)

### gzip.encode(data: string, options?: table) → string, error

Compresses data using GZIP.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to compress (supports binary) |
| options | table | no | nil | Compression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| level | integer | 6 | Compression level (1-9) |

**Returns:** `string` - Compressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Level out of range (1-9) | errors.INVALID | no |
| Compression failed | errors.INTERNAL | no |

### gzip.decode(data: string, options?: table) → string, error

Decompresses GZIP data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | GZIP compressed string |
| options | table | no | nil | Decompression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| max_size | integer | 134217728 | Maximum decompressed size in bytes (128MB), max 1GB |

**Returns:** `string` - Decompressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Invalid GZIP data | errors.INVALID | no |
| Decompressed size exceeds limit | errors.INTERNAL | no |

### deflate.encode(data: string, options?: table) → string, error

Compresses data using DEFLATE.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to compress (supports binary) |
| options | table | no | nil | Compression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| level | integer | 6 | Compression level (1-9) |

**Returns:** `string` - Compressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Level out of range (1-9) | errors.INVALID | no |
| Compression failed | errors.INTERNAL | no |

### deflate.decode(data: string, options?: table) → string, error

Decompresses DEFLATE data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | DEFLATE compressed string |
| options | table | no | nil | Decompression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| max_size | integer | 134217728 | Maximum decompressed size in bytes (128MB), max 1GB |

**Returns:** `string` - Decompressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Invalid DEFLATE data | errors.INVALID | no |
| Decompressed size exceeds limit | errors.INTERNAL | no |

### zlib.encode(data: string, options?: table) → string, error

Compresses data using zlib.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to compress (supports binary) |
| options | table | no | nil | Compression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| level | integer | 6 | Compression level (1-9) |

**Returns:** `string` - Compressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Level out of range (1-9) | errors.INVALID | no |
| Compression failed | errors.INTERNAL | no |

### zlib.decode(data: string, options?: table) → string, error

Decompresses zlib data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | zlib compressed string |
| options | table | no | nil | Decompression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| max_size | integer | 134217728 | Maximum decompressed size in bytes (128MB), max 1GB |

**Returns:** `string` - Decompressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Invalid zlib data | errors.INVALID | no |
| Decompressed size exceeds limit | errors.INTERNAL | no |

### brotli.encode(data: string, options?: table) → string, error

Compresses data using Brotli.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to compress (supports binary) |
| options | table | no | nil | Compression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| level | integer | 6 | Compression level (0-11) |

**Returns:** `string` - Compressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Level out of range (0-11) | errors.INVALID | no |
| Compression failed | errors.INTERNAL | no |

### brotli.decode(data: string, options?: table) → string, error

Decompresses Brotli data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Brotli compressed string |
| options | table | no | nil | Decompression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| max_size | integer | 134217728 | Maximum decompressed size in bytes (128MB), max 1GB |

**Returns:** `string` - Decompressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Invalid Brotli data | errors.INVALID | no |
| Decompressed size exceeds limit | errors.INTERNAL | no |

### zstd.encode(data: string, options?: table) → string, error

Compresses data using Zstandard.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | String to compress (supports binary) |
| options | table | no | nil | Compression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| level | integer | 3 | Compression level (1-22) |
| dict | string | nil | Dictionary bytes from `zstd.train_dict` |

**Returns:** `string` - Compressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Level out of range (1-22) | errors.INVALID | no |
| `dict` present but not a non-empty string | errors.INVALID | no |
| Compression failed | errors.INTERNAL | no |

**Notes:**
- Level 1-3: fastest compression
- Level 4-6: default compression
- Level 7-9: better compression
- Level 10-22: best compression
- When `dict` is supplied, the decoder must use the same dictionary

### zstd.decode(data: string, options?: table) → string, error

Decompresses Zstandard data.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Zstandard compressed string |
| options | table | no | nil | Decompression options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| max_size | integer | 134217728 | Maximum decompressed size in bytes (128MB), max 1GB |
| dict | string | nil | Dictionary bytes (must match the dict used to encode) |

**Returns:** `string` - Decompressed data, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Invalid Zstandard data | errors.INVALID | no |
| `dict` present but not a non-empty string | errors.INVALID | no |
| Decompressed size exceeds limit | errors.INTERNAL | no |
| Wrong / missing dict for a dict-compressed frame | errors.INTERNAL | no |

### zstd.train_dict(samples: table, options?: table) → string, error

Trains a Zstandard dictionary from a set of representative samples. Use
`encode`/`decode` with `{ dict = result }` to get much smaller frames for small,
structurally-similar payloads (log lines, RPC frames, JSON events).

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| samples | table | yes | - | Array of non-empty strings; at least one must be >= 8 bytes |
| options | table | no | nil | Training options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| size | integer | 114688 | Target dictionary size in bytes (256 to 1048576) |
| id | integer | 0 | Dictionary id (uint32). `0` picks a random non-zero id |
| level | integer | (best) | Compression level (1-22) used while building the dictionary |

**Returns:** `string` - Serialized dictionary bytes, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| samples not a table | errors.INVALID | no |
| samples table is empty | errors.INVALID | no |
| sample entry not a string | errors.INVALID | no |
| sample entry is empty | errors.INVALID | no |
| no sample is at least 8 bytes long | errors.INVALID | no |
| `size` out of range (256..1048576) | errors.INVALID | no |
| `id` does not fit in uint32 | errors.INVALID | no |
| `level` out of range (1..22) | errors.INVALID | no |
| Training failed | errors.INTERNAL | no |

**Notes:**
- Dictionaries are most useful for small payloads (under a few KB) that share
  recurring structure. They do not help for already-random or already-compressed
  data.
- The returned bytes are an opaque zstd dictionary; pass them verbatim as
  `dict = ...` to `zstd.encode`/`zstd.decode`. They are also accepted by other
  zstd implementations.
- This wraps `dict.BuildZstdDict` from `github.com/klauspost/compress` with
  `HashBytes = 6` and `ZstdDictCompat = true`.

### zstd.inspect_dict(dict: string) → table, error

Inspects a serialized Zstandard dictionary and returns its metadata. Useful for
diagnostics and for asserting that two parties share the same dictionary.

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| dict | string | yes | Dictionary bytes (e.g. produced by `zstd.train_dict`) |

**Returns:** `table` with the fields below, or `nil, error` on failure:

| Field | Type | Notes |
|-------|------|-------|
| id | integer | Dictionary id (uint32, always non-zero for valid dicts) |
| content_size | integer | Size of the dictionary content section, in bytes |

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| Input not a string | errors.INVALID | no |
| Input is empty | errors.INVALID | no |
| Invalid dictionary bytes | errors.INVALID | no |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = compress.gzip.encode(data)
if err then
    if err:kind() == errors.INVALID then
        -- bad input type, empty input, or invalid compression level
    elseif err:kind() == errors.INTERNAL then
        -- compression/decompression failed
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local compress = require("compress")

-- Basic gzip compression
local data = "Hello, World! This is a test string for compression."
local compressed, err = compress.gzip.encode(data)
if err then error(err) end

local decompressed, err = compress.gzip.decode(compressed)
if err then error(err) end
print(decompressed)  -- "Hello, World! This is a test string for compression."

-- With compression level
local fast, _ = compress.gzip.encode(data, { level = 1 })
local small, _ = compress.gzip.encode(data, { level = 9 })

-- Different algorithms
local br = compress.brotli.encode(data, { level = 11 })
local zs = compress.zstd.encode(data, { level = 22 })
local df = compress.deflate.encode(data)
local zl = compress.zlib.encode(data)

-- Binary data round-trip
local binary = "binary\x00data\xff"
local enc, _ = compress.zstd.encode(binary)
local dec, _ = compress.zstd.decode(enc)
assert(dec == binary)

-- Error handling
local result, err = compress.gzip.decode("invalid data")
if err then
    if err:kind() == errors.INVALID then
        print("Invalid input provided")
    end
end

-- With decompression size limit
local huge_compressed, _ = compress.gzip.encode(string.rep("data", 1000000))
local limited, err = compress.gzip.decode(huge_compressed, { max_size = 1024 })
if err then
    print("Decompressed size exceeds 1KB limit")
end
```
