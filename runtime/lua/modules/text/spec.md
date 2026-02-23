<!-- SPDX-License-Identifier: MPL-2.0 -->

# text

Text processing including regular expressions, diff/patch operations, and text splitting. Deterministic.

## Loading

```lua
local text = require("text")
```

## Functions

The text module has three submodules: `text.regexp`, `text.diff`, and `text.splitter`.

## text.regexp

### compile(pattern: string) → Regexp, error

Compiles a regular expression pattern using RE2 syntax.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| pattern | string | yes | - | RE2 compatible regex pattern |

**Returns:**
- Success: `regexp: Regexp` - compiled regexp object
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid pattern syntax | errors.INVALID | no |

## text.diff

### new(options?: table) → Differ, error

Creates a new differ instance for comparing and patching text.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | no | nil | Configuration options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| diff_timeout | number | 1.0 | Timeout in seconds |
| diff_edit_cost | integer | 4 | Cost of empty edit |
| match_threshold | number | 0.5 | Match tolerance 0-1 |
| match_distance | integer | 1000 | Distance to search |
| patch_delete_threshold | number | 0.5 | Delete threshold 0-1 |
| patch_margin | integer | 4 | Patch context margin |

**Returns:**
- Success: `differ: Differ` - differ object
- Error: `nil, error` - error is structured

## text.splitter

### recursive(options?: table) → Splitter, error

Creates a recursive character text splitter.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | no | nil | Configuration options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| chunk_size | integer | 4000 | Maximum characters per chunk |
| chunk_overlap | integer | 200 | Overlap between chunks |
| keep_separator | boolean | false | Keep separators in output |
| separators | string[] | - | Array of separator strings |

**Returns:**
- Success: `splitter: Splitter` - splitter object
- Error: `nil, error` - error is structured

### markdown(options?: table) → Splitter, error

Creates a markdown-aware text splitter.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | no | nil | Configuration options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| chunk_size | integer | 4000 | Maximum characters per chunk |
| chunk_overlap | integer | 200 | Overlap between chunks |
| code_blocks | boolean | false | Preserve code blocks |
| reference_links | boolean | false | Handle reference-style links |
| heading_hierarchy | boolean | false | Respect heading hierarchy |
| join_table_rows | boolean | false | Keep table rows together |
| separators | string[] | - | Array of separator strings |

**Returns:**
- Success: `splitter: Splitter` - splitter object
- Error: `nil, error` - error is structured

## Types

### Regexp

Returned by `text.regexp.compile()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| match_string | (s: string) | boolean | True if string matches pattern |
| find_string | (s: string) | string \| nil | First match or nil |
| find_all_string | (s: string) | string[] | All matches as array |
| find_string_submatch | (s: string) | string[] \| nil | Full match + capture groups or nil |
| find_all_string_submatch | (s: string) | string[][] | Array of matches, each with full match + groups |
| find_string_index | (s: string) | integer[] \| nil | {start, end} 1-based indices or nil |
| find_all_string_index | (s: string) | integer[][] \| nil | Array of {start, end} pairs or nil |
| replace_all_string | (s: string, replacement: string) | string | Replace all matches |
| split | (s: string, n?: integer) | string[] | Split by pattern, n limits parts (-1 for all) |
| num_subexp | () | integer | Number of capture groups |
| subexp_names | () | string[] | Array of group names (empty for unnamed) |
| string | () | string | Returns original pattern |

#### regexp:find_string_index(s: string) → integer[] | nil

Returns 1-based indices where match starts and ends. Use with `string.sub`:

```lua
local re, _ = text.regexp.compile("page\\d+")
local idx = re:find_string_index("The page1 here")
-- idx = {5, 9}
local match = content:sub(idx[1], idx[2])  -- "page1"
```

### Differ

Returned by `text.diff.new()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| compare | (text1: string, text2: string) | table, error | Array of diff objects |
| summarize | (diffs: table) | table | Statistics about diffs |
| pretty_text | (diffs: table) | string, error | Human-readable diff with ANSI |
| pretty_html | (diffs: table) | string, error | HTML formatted diff |
| patch_make | (text1: string, text2: string) | table, error | Create patches |
| patch_apply | (patches: table, text: string) | string, boolean | Apply patches, returns result + success |

#### differ:compare(text1: string, text2: string) → table, error

Returns array of diff objects with `operation` and `text` fields:

```lua
{
    {operation = "equal", text = "common"},
    {operation = "delete", text = "removed"},
    {operation = "insert", text = "added"}
}
```

Operations: `"equal"`, `"delete"`, `"insert"`

#### differ:summarize(diffs: table) → table

Returns statistics:

```lua
{
    insertions = 5,  -- characters inserted
    deletions = 3,   -- characters deleted
    equals = 10      -- characters unchanged
}
```

#### differ:patch_make(text1: string, text2: string) → table, error

Returns array of patch objects with `text` field.

#### differ:patch_apply(patches: table, text: string) → string, boolean

Returns patched text and success boolean. Success is true only if all patches applied successfully.

### Splitter

Returned by `text.splitter.recursive()` or `text.splitter.markdown()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| split_text | (text: string) | string[], error | Split text into chunks |
| split_batch | (pages: table) | table, error | Split multiple pages with metadata |

#### splitter:split_batch(pages: table) → table, error

Pages array must contain objects with:
- `content: string` - text to split
- `metadata: table` - optional metadata (preserved in output)

Returns array of chunk objects with `content` and `metadata` fields.

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local re, err = text.regexp.compile("[invalid")
if err then
    if err:kind() == errors.INVALID then
        -- handle invalid pattern
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local text = require("text")

-- Regular expressions
local re, err = text.regexp.compile("[0-9]+")
if err then error(err) end

local matches = re:find_all_string("a1b2c3")
-- {"1", "2", "3"}

local parts = re:split("a,b,c")
-- {"a", "b", "c"}

-- Email extraction with groups
local email_re, _ = text.regexp.compile("([a-z]+)@([a-z]+\\.[a-z]+)")
local match = email_re:find_string_submatch("user@example.com")
-- {"user@example.com", "user", "example.com"}

-- Text diffing
local diff, _ = text.diff.new()
local diffs, _ = diff:compare("hello world", "hello there")

local summary = diff:summarize(diffs)
print("Inserted:", summary.insertions)

-- Patching
local text1 = "The quick brown fox"
local text2 = "The quick red fox"
local patches, _ = diff:patch_make(text1, text2)
local result, ok = diff:patch_apply(patches, text1)

-- Text splitting
local splitter, _ = text.splitter.recursive({
    chunk_size = 100,
    chunk_overlap = 20
})
local chunks, _ = splitter:split_text("Long text content...")

-- Batch splitting with metadata
local pages = {
    {content = "Page 1", metadata = {page = 1}},
    {content = "Page 2", metadata = {page = 2}}
}
local batch_chunks, _ = splitter:split_batch(pages)
```
