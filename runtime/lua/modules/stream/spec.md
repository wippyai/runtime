<!-- SPDX-License-Identifier: MPL-2.0 -->

# stream

Stream read/write operations. IO, nondeterministic.

## Types

Stream module does not export module-level functions. Stream objects are obtained from other modules (e.g., `http.request():stream()`, `fs.open()`).

### Stream

Stream object for reading, writing, and seeking through data.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| read | (size?: integer) | string, error | Yields. size=0 reads all available |
| write | (data: string) | integer, error | Yields. Returns bytes written |
| seek | (whence?: string, offset?: integer) | integer, error | Yields. Returns new position |
| flush | () | boolean, error | Yields. Returns true on success |
| stat | () | table, error | Yields. Returns stream info |
| close | () | boolean, error | Yields. Returns true on success |
| scanner | (split?: string) | Scanner, error | Yields. Creates scanner for tokenization |

#### stream:read(size?: integer) → string, error

Reads data from stream.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| size | integer | no | 0 | Bytes to read. 0 = read all available |

**Returns:**
- Success: `string, nil` - data read (may be empty string)
- EOF: `nil, nil` - end of stream
- Error: `nil, error` - structured error

**Yields:** until data available or EOF

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| stream closed | errors.INTERNAL | no |
| read failure | errors.INTERNAL | no |

#### stream:write(data: string) → integer, error

Writes data to stream.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Data to write |

**Returns:**
- Success: `integer, nil` - number of bytes written
- Error: `0, error` - structured error

**Yields:** until write completes

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| stream closed | errors.INTERNAL | no |
| stream not writable | errors.INTERNAL | no |
| write failure | errors.INTERNAL | no |

#### stream:seek(whence?: string, offset?: integer) → integer, error

Seeks to position in stream.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| whence | string | no | "set" | "set" (from start), "cur" (from current), "end" (from end) |
| offset | integer | no | 0 | Offset in bytes |

**Returns:**
- Success: `integer, nil` - new position from start
- Error: `-1, error` - structured error

**Yields:** until seek completes

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid whence | errors.INVALID | no |
| stream not seekable | errors.INTERNAL | no |
| seek failure | errors.INTERNAL | no |

#### stream:flush() → boolean, error

Flushes buffered data to underlying storage.

**Returns:**
- Success: `true, nil`
- Error: `false, error` - structured error

**Yields:** until flush completes

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| stream closed | errors.INTERNAL | no |
| flush failure | errors.INTERNAL | no |

#### stream:stat() → table, error

Returns stream metadata and capabilities.

**Returns:**
- Success: `table, nil` - info table with fields below
- Error: `nil, error` - structured error

**Info table fields:**

| Field | Type | Notes |
|-------|------|-------|
| size | integer | Total size in bytes (-1 if unknown) |
| position | integer | Current position in bytes |
| readable | boolean | Stream supports reading |
| writable | boolean | Stream supports writing |
| seekable | boolean | Stream supports seeking |

**Yields:** until stat completes

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| stream closed | errors.INTERNAL | no |
| stat failure | errors.INTERNAL | no |

#### stream:close() → boolean, error

Closes stream and releases resources.

**Returns:**
- Success: `true, nil`
- Error: `false, error` - structured error

**Yields:** until close completes

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| close failure | errors.INTERNAL | no |

**Notes:**
- Safe to call multiple times
- Further operations after close will fail

#### stream:scanner(split?: string) → Scanner, error

Creates a scanner for tokenizing stream content.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| split | string | no | "lines" | Split mode: "lines", "words", "bytes", "runes" |

**Returns:**
- Success: `Scanner, nil` - scanner object
- Error: `nil, error` - structured error

**Yields:** until scanner created

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid split type | errors.INVALID | no |
| stream closed | errors.INTERNAL | no |
| scanner creation failure | errors.INTERNAL | no |

### Scanner

Scanner for tokenizing stream content. Returned by `stream:scanner()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| scan | () | boolean, error | Yields. Returns true if token available |
| text | () | string | Returns last scanned token text |
| err | () | string\|nil | Returns scanner error if any |

#### scanner:scan() → boolean, error

Scans for next token.

**Returns:**
- Success with token: `true, nil` - token available via `text()`
- Success at EOF: `false, nil` - no more tokens
- Error: `false, error` - structured error

**Yields:** until next token scanned or EOF

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| scanner closed | errors.INTERNAL | no |
| scan failure | errors.INTERNAL | no |

**Notes:**
- Must call before `text()` to advance scanner
- Returns false at EOF without error
- Updates internal state for `text()` and `err()`

#### scanner:text() → string

Returns text of last scanned token.

**Returns:** `string` - token text (empty if no token scanned yet)

**Notes:**
- Non-yielding, synchronous operation
- Returns last token from previous `scan()` call
- Empty string if `scan()` not called or returned false

#### scanner:err() → string|nil

Returns scanner error if any occurred during scanning.

**Returns:** `string|nil` - error message or nil if no error

**Notes:**
- Non-yielding, synchronous operation
- Returns error from last `scan()` call
- Nil if no error occurred

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local data, err = stream:read(100)
if err then
    if err:kind() == errors.INTERNAL then
        -- internal stream error
    elseif err:kind() == errors.INVALID then
        -- invalid parameters
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
-- Stream obtained from HTTP request
local http = require("http")

local function handler()
    local req = http.request()
    local res = http.response()

    -- Get request body as stream
    local stream, err = req:stream()
    if err then error(err) end

    -- Read chunks
    local chunks = {}
    while true do
        local chunk, read_err = stream:read(1024)
        if read_err then break end
        if chunk == nil then break end  -- EOF
        table.insert(chunks, chunk)
    end
    stream:close()

    -- Write response
    res:set_status(http.STATUS.OK)
    for _, chunk in ipairs(chunks) do
        res:write(chunk)
    end
end

-- Scanner example with file stream
local fs = require("fs")

local vol = fs.get("app:temp")
local file, err = vol:open("/data.txt", "r")
if err then error(err) end

-- Create line scanner
local scanner, err = file:scanner("lines")
if err then error(err) end

-- Scan all lines
while true do
    local has_token, err = scanner:scan()
    if err then error(err) end
    if not has_token then break end  -- EOF

    local line = scanner:text()
    print(line)
end

file:close()
```
