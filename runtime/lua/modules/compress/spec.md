# Lua Compress Module Specification

## Overview

The `compress` module provides compression and decompression functions for multiple algorithms including gzip, deflate, zlib, brotli, and zstd.

## Module Interface

### Module Loading

```lua
local compress = require("compress")
```

### Compression Algorithms

#### compress.gzip.encode(data: string, options?: table)

Compresses data using gzip algorithm.

Parameters:
- `data`: String data to compress
- `options`: Optional table with:
  - `level`: Compression level (1-9, default: 6)

Returns:
- `compressed`: Compressed data as string (or nil on error)
- `error`: Error message (or nil on success)

#### compress.gzip.decode(data: string)

Decompresses gzip-compressed data.

Parameters:
- `data`: Gzip-compressed string

Returns:
- `decompressed`: Decompressed data as string (or nil on error)
- `error`: Error message (or nil on success)

#### compress.deflate.encode(data: string, options?: table)

Compresses data using deflate algorithm.

Parameters:
- `data`: String data to compress
- `options`: Optional table with compression level

Returns:
- `compressed`: Compressed data as string (or nil on error)
- `error`: Error message (or nil on success)

#### compress.deflate.decode(data: string)

Decompresses deflate-compressed data.

Parameters:
- `data`: Deflate-compressed string

Returns:
- `decompressed`: Decompressed data as string (or nil on error)
- `error`: Error message (or nil on success)

#### compress.zlib.encode(data: string, options?: table)

Compresses data using zlib algorithm.

Parameters:
- `data`: String data to compress
- `options`: Optional table with compression level

Returns:
- `compressed`: Compressed data as string (or nil on error)
- `error`: Error message (or nil on success)

#### compress.zlib.decode(data: string)

Decompresses zlib-compressed data.

Parameters:
- `data`: Zlib-compressed string

Returns:
- `decompressed`: Decompressed data as string (or nil on error)
- `error`: Error message (or nil on success)

#### compress.brotli.encode(data: string, options?: table)

Compresses data using brotli algorithm.

Parameters:
- `data`: String data to compress
- `options`: Optional table with compression level

Returns:
- `compressed`: Compressed data as string (or nil on error)
- `error`: Error message (or nil on success)

#### compress.brotli.decode(data: string)

Decompresses brotli-compressed data.

Parameters:
- `data`: Brotli-compressed string

Returns:
- `decompressed`: Decompressed data as string (or nil on success)
- `error`: Error message (or nil on success)

#### compress.zstd.encode(data: string, options?: table)

Compresses data using zstd algorithm.

Parameters:
- `data`: String data to compress
- `options`: Optional table with compression level

Returns:
- `compressed`: Compressed data as string (or nil on error)
- `error`: Error message (or nil on success)

#### compress.zstd.decode(data: string)

Decompresses zstd-compressed data.

Parameters:
- `data`: Zstd-compressed string

Returns:
- `decompressed`: Decompressed data as string (or nil on error)
- `error`: Error message (or nil on success)

## Example Usage

```lua
local compress = require("compress")

-- Gzip compression
local data = "Hello, World!"
local compressed, err = compress.gzip.encode(data)
if err then
  print("Compression error:", err)
  return
end

local decompressed, err = compress.gzip.decode(compressed)
if err then
  print("Decompression error:", err)
  return
end

print("Original:", data)
print("Decompressed:", decompressed)

-- Custom compression level
compressed, err = compress.gzip.encode(data, {level = 9})

-- Using other algorithms
compressed = compress.deflate.encode(data)
compressed = compress.zlib.encode(data)
compressed = compress.brotli.encode(data)
compressed = compress.zstd.encode(data)
```
