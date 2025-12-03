# Lua Text Module Specification

## Overview

The `text` module provides text processing utilities including regular expressions, diff, and patch operations.

## Module Interface

### Module Loading

```lua
local text = require("text")
```

### Submodules

- `text.regexp` - Regular expression operations
- `text.diff` - Diff and patch operations
- `text.splitter` - Text splitting for chunking

## Regular Expression API

### text.regexp.compile(pattern: string)

Compiles a regular expression pattern.

Parameters:

- `pattern`: RE2 compatible regex pattern string.

Returns:

- `regexp`: Compiled regexp object (or nil on error).
- `error`: Structured error object (or nil on success).

### Regexp Methods

#### regexp:match_string(s: string) -> boolean

Returns true if the string matches the pattern.

#### regexp:find_string(s: string) -> string|nil

Returns the first match or nil.

#### regexp:find_all_string(s: string) -> table

Returns array of all matches.

#### regexp:find_string_submatch(s: string) -> table|nil

Returns array with full match and capture groups, or nil.

#### regexp:find_all_string_submatch(s: string) -> table

Returns array of arrays, each with full match and capture groups.

#### regexp:find_string_index(s: string) -> table|nil

Returns `{start, end}` indices (1-based start) or nil.

#### regexp:find_all_string_index(s: string) -> table|nil

Returns array of `{start, end}` index pairs.

#### regexp:replace_all_string(s: string, replacement: string) -> string

Replaces all matches with replacement string.

#### regexp:split(s: string, n?: number) -> table

Splits string by pattern. Optional n limits number of parts.

#### regexp:num_subexp() -> number

Returns number of capture groups in pattern.

#### regexp:subexp_names() -> table

Returns array of capture group names (empty string for unnamed).

#### regexp:string() -> string

Returns the original pattern string.

## Diff API

### text.diff.new(options?: table)

Creates a new differ instance.

Parameters:

- `options`: Optional configuration table:
  - `diff_timeout`: Timeout in seconds (default: 1.0)
  - `diff_edit_cost`: Cost of empty edit (default: 4)
  - `match_threshold`: Match tolerance 0-1 (default: 0.5)
  - `match_distance`: Distance to search (default: 1000)
  - `patch_delete_threshold`: Delete threshold 0-1 (default: 0.5)
  - `patch_margin`: Patch context margin (default: 4)

Returns:

- `differ`: Differ object (or nil on error).
- `error`: Structured error object (or nil on success).

### Differ Methods

#### differ:compare(text1: string, text2: string) -> table, error

Compares two texts and returns diff operations.

Returns array of diff objects:
```lua
{
    {operation = "equal", text = "common"},
    {operation = "delete", text = "removed"},
    {operation = "insert", text = "added"}
}
```

Operations: `"equal"`, `"delete"`, `"insert"`

#### differ:summarize(diffs: table) -> table

Returns statistics about diff operations.

```lua
{
    insertions = 5,  -- characters inserted
    deletions = 3,   -- characters deleted
    equals = 10      -- characters unchanged
}
```

#### differ:pretty_text(diffs: table) -> string, error

Returns human-readable diff with ANSI colors.

#### differ:pretty_html(diffs: table) -> string, error

Returns HTML formatted diff.

#### differ:patch_make(text1: string, text2: string) -> table, error

Creates patches to transform text1 into text2.

Returns array of patch objects with `text` field.

#### differ:patch_apply(patches: table, text: string) -> string, boolean

Applies patches to text.

Returns:
- `result`: Patched text
- `success`: true if all patches applied successfully

## Splitter API

### text.splitter.recursive(options?: table)

Creates a recursive character text splitter.

Parameters:

- `options`: Optional configuration table:
  - `chunk_size`: Maximum characters per chunk (default: 4000)
  - `chunk_overlap`: Overlap between chunks (default: 200)
  - `keep_separator`: Keep separators in output (default: false)
  - `separators`: Array of separator strings

Returns:

- `splitter`: Splitter object (or nil on error).
- `error`: Structured error object (or nil on success).

### text.splitter.markdown(options?: table)

Creates a markdown-aware text splitter.

Parameters:

- `options`: Optional configuration table:
  - `chunk_size`: Maximum characters per chunk (default: 4000)
  - `chunk_overlap`: Overlap between chunks (default: 200)
  - `code_blocks`: Preserve code blocks (default: false)
  - `reference_links`: Handle reference-style links (default: false)
  - `heading_hierarchy`: Respect heading hierarchy (default: false)
  - `join_table_rows`: Keep table rows together (default: false)
  - `separators`: Array of separator strings

Returns:

- `splitter`: Splitter object (or nil on error).
- `error`: Structured error object (or nil on success).

### Splitter Methods

#### splitter:split_text(text: string) -> table, error

Splits text into chunks.

Returns:

- `chunks`: Array of text strings.
- `error`: Structured error object (or nil on success).

#### splitter:split_batch(pages: table) -> table, error

Splits multiple pages with metadata preservation.

Parameters:

- `pages`: Array of page objects:
  - `content`: Text content to split
  - `metadata`: Optional metadata table (preserved in output)

Returns:

- `chunks`: Array of chunk objects with `content` and `metadata` fields.
- `error`: Structured error object (or nil on success).

## Error Handling

The module returns structured errors using the `lua.Error` type.

### Error Types

1. **Invalid Pattern:** If regex pattern is invalid.

```lua
local re, err = text.regexp.compile("[invalid")
-- re: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
```

### Error Kind Comparison

Always use `errors.*` constants for kind comparison:

```lua
local re, err = text.regexp.compile(pattern)
if err then
    if err:kind() == errors.INVALID then
        -- handle invalid pattern
    end
end
```

## Example Usage

```lua
local text = require("text")

-- Regular expressions
local re, err = text.regexp.compile("[0-9]+")
if err then
    print("Error:", err)
    return
end

local matches = re:find_all_string("a1b2c3")
-- {"1", "2", "3"}

local parts = text.regexp.compile(","):split("a,b,c")
-- {"a", "b", "c"}

-- Email extraction with groups
local email_re = text.regexp.compile("([a-z]+)@([a-z]+\\.com)")
local match = email_re:find_string_submatch("user@example.com")
-- {"user@example.com", "user", "example.com"}

-- Text diffing
local diff = text.diff.new()
local diffs = diff:compare("hello world", "hello there")

local summary = diff:summarize(diffs)
print("Inserted:", summary.insertions)
print("Deleted:", summary.deletions)

-- Pretty output
local pretty = diff:pretty_text(diffs)
print(pretty)

-- Patching
local text1 = "The quick brown fox"
local text2 = "The quick red fox"
local patches = diff:patch_make(text1, text2)
local result, ok = diff:patch_apply(patches, text1)
assert(result == text2)

-- Text splitting
local splitter = text.splitter.recursive({chunk_size = 100, chunk_overlap = 20})
local chunks = splitter:split_text("Long text content...")

-- Markdown splitting
local md_splitter = text.splitter.markdown({
    chunk_size = 500,
    heading_hierarchy = true
})
local md_chunks = md_splitter:split_text("# Heading\n\nContent...")

-- Batch splitting with metadata
local pages = {
    {content = "Page 1 content", metadata = {page = 1}},
    {content = "Page 2 content", metadata = {page = 2}}
}
local batch_chunks = splitter:split_batch(pages)
-- Each chunk has content and preserved metadata
```

## Thread Safety

- The `text` module is thread-safe.
- Module tables are immutable and shared across Lua states.
- Regexp and Differ objects are stateless for read operations.

## Module Classification

- **Class**: `deterministic`
- Operations are pure functions with no side effects.
- Same input always produces the same output.

## Implementation Notes

- Uses Go's `regexp` package (RE2 syntax).
- Uses `github.com/sergi/go-diff/diffmatchpatch` for diff operations.
- Uses `github.com/tmc/langchaingo/textsplitter` for text splitting.
- Module uses `ModuleDef` struct for definition.
- Type methods registered via `value.RegisterTypeMethods`.

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "text",
    Description: "Text processing: regex, diff, and patch operations",
    Class:       []string{luaapi.ClassDeterministic},
    Build:       buildModule,
}
```
