<!-- SPDX-License-Identifier: MPL-2.0 -->

# fs

Filesystem operations with sandboxed access to named filesystem registries. Storage, IO, nondeterministic.

## Loading

```lua
local fs = require("fs")
```

## Module Functions

### get(name: string) → FS, error

Retrieves a filesystem instance by name from the registry.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Filesystem name in registry |

**Returns:** FS object and nil error on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |
| empty name | errors.INVALID | no |
| security policy denial | errors.PERMISSION_DENIED | no |
| no filesystem registry in context | errors.INTERNAL | no |
| filesystem not found | errors.NOT_FOUND | no |

**Notes:**
- Security policies control access via `fs.get` permission
- Filesystem instances maintain working directory state
- Each FS object starts with working directory at root `/`

## Constants

### type

File entry type constants.

| Constant | Value | Description |
|----------|-------|-------------|
| type.FILE | "file" | Regular file |
| type.DIR | "directory" | Directory |

### seek

File seek position constants.

| Constant | Value | Description |
|----------|-------|-------------|
| seek.SET | "set" | Absolute position from start |
| seek.CUR | "cur" | Relative to current position |
| seek.END | "end" | Relative to end of file |

## FS Object Methods

### chdir(path: string) → boolean, error

Changes the working directory.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | Absolute or relative path |

**Returns:** `true, nil` on success, or `false, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty path | errors.INVALID | no |
| path contains null byte | errors.INVALID | no |
| path not found | errors.NOT_FOUND | no |
| path is not a directory | errors.INVALID | no |

**Notes:**
- Affects all subsequent path operations on this FS object
- Relative paths are resolved from current working directory
- Absolute paths (starting with `/`) ignore working directory

### pwd() → string, error

Returns the current working directory.

**Returns:** Current working directory path (always starts with `/`), nil error (never fails)

**Notes:**
- Returns `/` for root directory
- Path is always absolute

### open(path: string, mode: string) → File, error

Opens a file for reading, writing, or appending.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | File path (absolute or relative) |
| mode | string | yes | - | Open mode: "r", "w", "wx", "a" |

**Modes:**
- `"r"` - Read-only, file must exist
- `"w"` - Write, create or truncate existing
- `"wx"` - Write exclusive, fails if file exists
- `"a"` - Append, create if doesn't exist

**Returns:** File object and nil error on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |
| empty path | errors.INVALID | no |
| path contains null byte | errors.INVALID | no |
| invalid mode | errors.INVALID | no |
| file not found (read mode) | errors.NOT_FOUND | no |

**Notes:**
- Files are automatically closed on function return via cleanup
- Use `:close()` for explicit cleanup
- File permissions: created files have mode 0644

### stat(path: string) → table, error

Gets file or directory information.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | File or directory path |

**Returns:** File info table and nil error on success, or `nil, error` on failure

**Info table fields:**
- `name` (string) - File/directory name
- `size` (number) - Size in bytes
- `mode` (number) - Unix file mode
- `modified` (number) - Modification time (Unix timestamp)
- `is_dir` (boolean) - true if directory
- `type` (string) - "file" or "directory" (use fs.type constants)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty path | errors.INVALID | no |
| path contains null byte | errors.INVALID | no |
| path not found | errors.NOT_FOUND | no |

### mkdir(path: string) → boolean, error

Creates a directory.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | Directory path to create |

**Returns:** `true, nil` on success, or `false, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty path | errors.INVALID | no |
| path contains null byte | errors.INVALID | no |
| path already exists | errors.ALREADY_EXISTS | no |
| mkdir failed | errors.INTERNAL | no |

**Notes:**
- Does not create parent directories (not recursive)
- Created directories have mode 0755

### remove(path: string) → boolean, error

Removes a file or empty directory.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | File or directory path |

**Returns:** `true, nil` on success, or `false, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty path | errors.INVALID | no |
| path contains null byte | errors.INVALID | no |
| directory not empty | errors.INVALID | no |
| remove failed | errors.INTERNAL | no |

**Notes:**
- Directories must be empty to be removed
- Files and empty directories are removed atomically

### readdir(path: string) → iterator, state

Reads directory entries and returns an iterator.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | Directory path |

**Returns:** Iterator function and state for `for` loop, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty path | errors.INVALID | no |
| path contains null byte | errors.INVALID | no |
| path not found | errors.NOT_FOUND | no |
| path is not a directory | errors.INVALID | no |
| readdir failed | errors.INTERNAL | no |

**Entry table fields:**
- `name` (string) - Entry name
- `type` (string) - "file" or "directory" (use fs.type constants)

**Notes:**
- Use with `for` loop: `for entry in iter, state do ... end`
- Empty directories return valid iterator with no entries

### exists(path: string) → boolean, error

Checks if path exists.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | File or directory path |

**Returns:** `true` if exists, `false` if not, plus nil error on success or error on invalid path

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| path contains null byte | errors.INVALID | no |

**Notes:**
- Returns `false, nil` for non-existent paths (not an error)
- Returns `false, error` only for invalid paths

### isdir(path: string) → boolean, error

Checks if path is a directory.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | File or directory path |

**Returns:** `true` if directory, `false` if not, or error on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| path contains null byte | errors.INVALID | no |
| path not found | errors.NOT_FOUND | no |

### readfile(path: string) → string, error

Reads entire file contents.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | File path |

**Returns:** File contents as string and nil error on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty path | errors.INVALID | no |
| path contains null byte | errors.INVALID | no |
| file not found | errors.NOT_FOUND | no |
| read failed | errors.INTERNAL | no |

**Notes:**
- Reads entire file into memory
- Binary data is preserved in returned string
- File is automatically closed after reading

### writefile(path: string, data: string|Reader, mode?: string) → boolean, error

Writes data to a file.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| path | string | yes | - | File path |
| data | string or Reader | yes | - | Data to write (string or stream) |
| mode | string | no | "w" | Write mode: "w", "wx", "a" |

**Modes:**
- `"w"` - Write, create or truncate existing
- `"wx"` - Write exclusive, fails if file exists
- `"a"` - Append to existing or create

**Returns:** `true, nil` on success, or `false, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty path | errors.INVALID | no |
| path contains null byte | errors.INVALID | no |
| data argument is nil | errors.INVALID | no |
| invalid mode | errors.INVALID | no |
| invalid input type | errors.INVALID | no |
| failed to get reader | errors.INTERNAL | no |
| failed to open destination | errors.NOT_FOUND | no |
| copy failed | errors.INTERNAL | no |

**Notes:**
- Accepts string or io.Reader (stream)
- Created files have mode 0644
- File is automatically closed after writing

## File Object Methods

### read(size?: integer) → string, error

Reads bytes from file.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| size | integer | no | 4096 | Number of bytes to read |

**Returns:** Data read as string and nil error on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| size <= 0 | errors.INVALID | no |
| EOF reached | errors.NOT_FOUND | no |
| read failed | errors.INTERNAL | no |

**Notes:**
- Returns fewer bytes than requested if EOF is near
- EOF returns `nil, error` with kind NOT_FOUND

### write(data: string) → boolean, error

Writes string data to file.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Data to write |

**Returns:** `true, nil` on success, or `false, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| empty data | errors.INVALID | no |
| write failed | errors.INTERNAL | no |

### seek(whence: string, offset: integer) → number, error

Sets file position.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| whence | string | yes | - | "set", "cur", or "end" |
| offset | integer | yes | - | Offset in bytes |

**Whence values:**
- `"set"` or `fs.seek.SET` - Absolute position from start
- `"cur"` or `fs.seek.CUR` - Relative to current position
- `"end"` or `fs.seek.END` - Relative to end of file

**Returns:** New position and nil error on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid whence | errors.INVALID | no |
| seek failed | errors.INTERNAL | no |

**Notes:**
- Returns new absolute position from start
- Use `seek("cur", 0)` to get current position

### close() → boolean, error

Closes the file.

**Returns:** `true, nil` on success, or `false, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| close failed | errors.INTERNAL | no |

**Notes:**
- Safe to call multiple times (subsequent calls are no-op)
- Files are auto-closed on function return

### stat() → table, error

Gets file information.

**Returns:** File info table (same structure as FS:stat) and nil error on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| stat failed | errors.INTERNAL | no |

### sync() → boolean, error

Flushes file changes to storage.

**Returns:** `true, nil` on success, or `false, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| sync failed | errors.INTERNAL | no |

**Notes:**
- Forces write of buffered data to storage
- Call before close for critical data

### scanner(split?: string) → Scanner, error

Creates a scanner for reading file by tokens.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| split | string | no | "lines" | Split type: "lines", "words", "bytes", "runes" |

**Split types:**
- `"lines"` - Split by line breaks
- `"words"` - Split by whitespace
- `"bytes"` - Single byte tokens
- `"runes"` - Single UTF-8 rune tokens

**Returns:** Scanner object and nil error on success, or `nil, error` on failure

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| file is closed | errors.INVALID | no |
| invalid split type | errors.INVALID | no |

**Notes:**
- Scanner reads from current file position
- Scanner shares file state with other operations

## Scanner Object Methods

### scan() → boolean

Advances to next token.

**Returns:** `true` if token available, `false` if EOF or error

**Notes:**
- Call `:text()` to get token after successful scan
- Call `:err()` to check for scan errors

### text() → string

Returns the most recent token from scan.

**Returns:** Token text as string

**Notes:**
- Returns last scanned token
- Empty string before first scan

### err() → string

Returns scanner error if any.

**Returns:** Error message as string, or nil if no error

**Notes:**
- Check after scan returns false
- nil means clean EOF, non-nil means error

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local vol, err = fs.get("myfs")
if err then
    error(tostring(err))
end

local file, err = vol:open("/test.txt", "r")
if err then
    if err:kind() == errors.NOT_FOUND then
        -- file doesn't exist
    elseif err:kind() == errors.INVALID then
        -- invalid arguments
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.NOT_FOUND`, `errors.ALREADY_EXISTS`, `errors.INTERNAL`

## Example

```lua
local fs = require("fs")

-- Get filesystem
local vol, err = fs.get("app:temp")
if err then
    error("Failed to get filesystem: " .. tostring(err))
end

-- Create directory structure
local ok, err = vol:mkdir("/data")
if err then
    error("mkdir failed: " .. tostring(err))
end

-- Write file
ok, err = vol:writefile("/data/config.txt", "key=value\nfoo=bar")
if err then
    error("writefile failed: " .. tostring(err))
end

-- Read file
local content, err = vol:readfile("/data/config.txt")
if err then
    error("readfile failed: " .. tostring(err))
end
print("Content:", content)

-- List directory
local iter, state = vol:readdir("/data")
if not iter then
    error("readdir failed: " .. tostring(state))
end

for entry in iter, state do
    print(entry.name, entry.type)
end

-- Work with files
local file, err = vol:open("/data/log.txt", "w")
if err then
    error("open failed: " .. tostring(err))
end

file:write("Log entry 1\n")
file:write("Log entry 2\n")
file:sync()
file:close()

-- Read with scanner
file, err = vol:open("/data/log.txt", "r")
if err then
    error("open failed: " .. tostring(err))
end

local scanner, err = file:scanner("lines")
if err then
    error("scanner failed: " .. tostring(err))
end

while scanner:scan() do
    print("Line:", scanner:text())
end

if scanner:err() then
    error("scan error: " .. scanner:err())
end

file:close()

-- Seek operations
file, err = vol:open("/data/config.txt", "r")
if err then
    error("open failed: " .. tostring(err))
end

local pos, err = file:seek("set", 10)
if err then
    error("seek failed: " .. tostring(err))
end

local data, err = file:read(5)
print("Data at position", pos, ":", data)

file:close()

-- Check existence
local exists, err = vol:exists("/data/config.txt")
print("File exists:", exists)

local isdir, err = vol:isdir("/data")
print("Is directory:", isdir)

-- Get file info
local info, err = vol:stat("/data/config.txt")
if not err then
    print("Size:", info.size)
    print("Modified:", info.modified)
    print("Type:", info.type)
end

-- Cleanup
vol:remove("/data/config.txt")
vol:remove("/data/log.txt")
vol:remove("/data")
```
