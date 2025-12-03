# Lua Compress Module Specification

## Overview

The `compress` module provides data compression and decompression functions supporting multiple algorithms: gzip, deflate, zlib, brotli, and zstd.

## Module Interface

### Module Loading

```lua
local compress = require("compress")
```

### Sub-modules

The module exposes algorithm-specific sub-tables:

- `compress.gzip` - GZIP compression (RFC 1952)
- `compress.deflate` - DEFLATE compression (RFC 1951)
- `compress.zlib` - zlib compression (RFC 1950)
- `compress.brotli` - Brotli compression (RFC 7932)
- `compress.zstd` - Zstandard compression (RFC 8878)

### Functions

Each sub-module provides identical function signatures:

#### {algorithm}.encode(data: string [, options: table])

Compresses a string using the specified algorithm.

Parameters:

- `data`: String to compress (must not be empty).
- `options` (optional): Table with compression options.
  - `level`: Compression level (algorithm-specific range).

Returns:

- `compressed`: Compressed data as string (or nil on error).
- `error`: Structured error object (or nil on success).

#### {algorithm}.decode(data: string)

Decompresses data using the specified algorithm.

Parameters:

- `data`: Compressed string to decompress (must not be empty).

Returns:

- `decompressed`: Original data as string (or nil on error).
- `error`: Structured error object (or nil on success).

### Compression Levels

| Algorithm | Default | Min | Max |
|-----------|---------|-----|-----|
| gzip      | 6       | 1   | 9   |
| deflate   | 6       | 1   | 9   |
| zlib      | 6       | 1   | 9   |
| brotli    | 6       | 0   | 11  |
| zstd      | 3       | 1   | 22  |

## Error Handling

The module returns structured errors using the `lua.Error` type. See [errors.md](../errors.md) for full error specification.

### Error Types

1. **Invalid Input Type:** If input is not a string.

```lua
local result, err = compress.gzip.encode(123)
-- result: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
-- tostring(err) == "string expected"
```

2. **Empty Input:** If input string is empty.

```lua
local result, err = compress.gzip.encode("")
-- result: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
-- tostring(err) contains "empty"
```

3. **Invalid Level:** If compression level is out of range.

```lua
local result, err = compress.gzip.encode("data", { level = 99 })
-- result: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
```

4. **Invalid Compressed Data:** If data cannot be decompressed.

```lua
local result, err = compress.gzip.decode("not valid gzip")
-- result: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
```

### Error Kind Comparison

Always use `errors.*` constants for kind comparison:

```lua
local result, err = compress.gzip.decode(input)
if err then
    if err:kind() == errors.INVALID then
        -- handle invalid input
    end
end
```

## Behavior

1. **Empty String Handling**
   - Empty strings are rejected with an error.
   - This differs from base64 which accepts empty strings.

2. **Binary Data**
   - The module handles binary data (strings with null bytes and non-ASCII characters).
   - Binary data is preserved during encode/decode round-trips.

3. **Compression Ratio**
   - Higher compression levels produce smaller output but take longer.
   - Highly compressible data (repeated patterns) achieves better ratios.
   - Already compressed or random data may not compress well.

## Thread Safety

- The `compress` module is thread-safe.
- It uses immutable module tables shared across Lua states.
- No internal mutable state is maintained.

## Module Classification

- **Class**: `encoding`, `deterministic`
- Operations are pure functions with no side effects.
- Same input always produces the same output.

## Example Usage

```lua
local compress = require("compress")

-- Basic gzip compression
local compressed, err = compress.gzip.encode("Hello, World!")
if err then
    print("Error:", err)
    return
end

-- Decompress
local original, err = compress.gzip.decode(compressed)
print(original)  -- "Hello, World!"

-- With compression level
local fast, _ = compress.gzip.encode("data", { level = 1 })
local small, _ = compress.gzip.encode("data", { level = 9 })

-- Different algorithms
local br = compress.brotli.encode("data")
local zs = compress.zstd.encode("data")
local df = compress.deflate.encode("data")
local zl = compress.zlib.encode("data")

-- Round-trip with binary data
local binary = "binary\x00data\xff"
local enc, _ = compress.zstd.encode(binary)
local dec, _ = compress.zstd.decode(enc)
assert(dec == binary)  -- true

-- Error handling
local result, err = compress.gzip.decode("invalid data")
if err then
    print("Decode failed:", err:message())
    if err:kind() == errors.INVALID then
        print("Invalid input provided")
    end
end
```

## Implementation Notes

- Uses Go standard library for gzip, deflate, zlib.
- Uses `github.com/andybalholm/brotli` for Brotli.
- Uses `github.com/klauspost/compress/zstd` for Zstandard.
- Module uses `ModuleDef` struct for definition.
- Module table is created once and shared across all Lua states.
- Errors include Lua stack traces for debugging.

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "compress",
    Description: "Data compression (gzip, deflate, zlib, brotli, zstd)",
    Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
    Build: func() (*lua.LTable, []luaapi.YieldType) {
        mod := &lua.LTable{}

        gzipTable := &lua.LTable{}
        gzipTable.RawSetString("encode", lua.LGoFunc(gzipEncode))
        gzipTable.RawSetString("decode", lua.LGoFunc(gzipDecode))
        gzipTable.Immutable = true
        mod.RawSetString("gzip", gzipTable)

        // ... similar for deflate, zlib, brotli, zstd

        mod.Immutable = true
        return mod, []luaapi.YieldType{}
    },
}
```
