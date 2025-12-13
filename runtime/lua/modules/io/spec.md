# io

Terminal IO operations for stdin, stdout, and stderr. IO, nondeterministic.

## Loading

```lua
local io = require("io")
```

## Functions

### write(...: string) → boolean, error

Writes strings to stdout without newline or separators.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ... | string | no | - | Variable number of strings to write |

**Returns:**
- Success: `true`
- Error: `nil, error` - error is string

**Errors (strings):**
- `"no terminal context"` - no terminal context available
- IO write error message - write operation failed

**Notes:**
- Takes variable number of arguments
- All arguments are converted to strings via `tostring()`
- No spaces or newlines are added between arguments
- No newline at end

### print(...: any) → boolean

Writes values to stdout with tabs between and newline at end.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ... | any | no | - | Variable number of values to print |

**Returns:** `true` (always, errors ignored)

**Errors:**
- Errors from terminal write are ignored
- Returns `nil, error` only if no terminal context available

**Notes:**
- Takes variable number of arguments
- All arguments are converted to strings via `tostring()`
- Arguments separated by tabs (`\t`)
- Newline appended at end

### eprint(...: any) → boolean

Writes values to stderr with tabs between and newline at end.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| ... | any | no | - | Variable number of values to print |

**Returns:** `true` (always, errors ignored)

**Errors:**
- Errors from terminal write are ignored
- Returns `nil, error` only if no terminal context available

**Notes:**
- Takes variable number of arguments
- All arguments are converted to strings via `tostring()`
- Arguments separated by tabs (`\t`)
- Newline appended at end

### read(n?: integer) → string, error

Reads up to n bytes from stdin.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| n | integer | no | 1024 | Number of bytes to read (values <= 0 become 1024) |

**Returns:**
- Success: `string` - bytes read (may be less than n)
- Error: `nil, error` - error is string

**Errors (strings):**
- `"no terminal context"` - no terminal context available
- IO read error message - read operation failed (e.g., EOF)

**Notes:**
- Returns actual bytes read, which may be less than requested
- Returns empty string if no data available

### readline() → string, error

Reads a line from stdin up to newline character.

**Returns:**
- Success: `string` - line without trailing newline/carriage return
- Error: `nil, error` - error is string

**Errors (strings):**
- `"no terminal context"` - no terminal context available
- IO read error message - read operation failed

**Notes:**
- Strips trailing `\n` and `\r` from result
- If EOF reached with partial data, returns partial line without error
- If EOF with no data, returns `nil, error`

### flush() → boolean, error

Flushes stdout if it supports flushing.

**Returns:**
- Success: `true`
- Error: `nil, error` - error is string

**Errors (strings):**
- `"no terminal context"` - no terminal context available
- Sync error message - flush operation failed

**Notes:**
- Only flushes if stdout implements `Sync()` method
- No-op on non-flushable streams (returns true)

### args() → string[]

Returns command line arguments as array.

**Returns:** `table` - array of string arguments (1-indexed), empty table if no terminal context

**Notes:**
- Never fails, returns empty table if no terminal context
- Arguments are 1-indexed Lua array

## Example

```lua
local io = require("io")

-- Get command line arguments
local args = io.args()
if #args > 0 then
    io.print("Arguments:", table.concat(args, ", "))
end

-- Prompt for input
io.write("Enter your name: ")
io.flush()
local name, err = io.readline()
if err then
    io.eprint("Error reading input:", err)
    return 1
end

io.print("Hello,", name)

-- Read fixed bytes
local data, err = io.read(10)
if err then
    io.eprint("Read error:", err)
else
    io.print("Read", #data, "bytes")
end
```
