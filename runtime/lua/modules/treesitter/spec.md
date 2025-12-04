# Lua Tree-sitter Module Specification

## Overview

The `treesitter` module provides Tree-sitter parsing and syntax analysis capabilities. Parse source code into ASTs and query them using Tree-sitter's pattern matching language.

## Module Interface

### Module Loading

```lua
local treesitter = require("treesitter")
```

## Functions

### treesitter.supported_languages()

Returns a table of supported language names.

Returns:
- `languages`: Table where keys are language names and values are `true`.

### treesitter.language(name)

Get a Language object for introspection.

Parameters:
- `name`: Language name string (e.g., "go", "lua", "javascript").

Returns:
- `language`: Language object (or nil on error).
- `error`: Structured error object (or nil on success).

### treesitter.parse(language, code)

Parse source code and return a Tree.

Parameters:
- `language`: Language name string.
- `code`: Source code string.

Returns:
- `tree`: Tree object (or nil on error).
- `error`: Structured error object (or nil on success).

### treesitter.parser()

Create a reusable Parser object.

Returns:
- `parser`: Parser object.

### treesitter.query(language, pattern)

Create a Query for pattern matching.

Parameters:
- `language`: Language name string.
- `pattern`: Tree-sitter query pattern string.

Returns:
- `query`: Query object (or nil on error).
- `error`: Structured error object (or nil on success).

## Parser Methods

### parser:set_language(name)

Set the parser's language.

Returns:
- `success`: Boolean.

### parser:get_language()

Get the current language name.

Returns:
- `name`: Language name string.

### parser:parse(code, [old_tree])

Parse code, optionally with a previous tree for incremental parsing.

Returns:
- `tree`: Tree object.

### parser:reset()

Reset the parser state.

### parser:close()

Close and release the parser.

### parser:set_timeout(microseconds)

Set parsing timeout.

### parser:set_ranges(ranges)

Set included ranges for parsing.

## Tree Methods

### tree:root_node()

Get the root node.

Returns:
- `node`: Node object.

### tree:root_node_with_offset(byte_offset, row, col)

Get root node with position offset.

### tree:walk()

Create a TreeCursor for traversal.

Returns:
- `cursor`: Cursor object.

### tree:copy()

Create a copy of the tree.

Returns:
- `tree`: New Tree object.

### tree:edit(edit)

Apply an edit to update the tree.

Parameters:
- `edit`: Table with keys: `start_byte`, `old_end_byte`, `new_end_byte`, `start_row`, `start_column`, `old_end_row`, `old_end_column`, `new_end_row`, `new_end_column`.

Returns:
- `success`: Boolean.

### tree:changed_ranges(other_tree)

Get ranges that changed between trees.

Returns:
- `ranges`: Array of range tables.

### tree:included_ranges()

Get included ranges.

Returns:
- `ranges`: Array of range tables.

### tree:language()

Get the tree's language.

### tree:dot_graph()

Get DOT graph representation.

Returns:
- `dot`: DOT graph string.

### tree:close()

Close and release the tree.

## Node Methods

### Navigation

- `node:parent()` - Get parent node.
- `node:child(index)` - Get child by index (0-based).
- `node:child_count()` - Get number of children.
- `node:named_child(index)` - Get named child by index.
- `node:named_child_count()` - Get number of named children.
- `node:child_by_field_name(name)` - Get child by field name.
- `node:field_name_for_child(index)` - Get field name for child at index.
- `node:next_sibling()` - Get next sibling.
- `node:prev_sibling()` - Get previous sibling.
- `node:next_named_sibling()` - Get next named sibling.
- `node:prev_named_sibling()` - Get previous named sibling.
- `node:descendant_count()` - Get total descendant count.
- `node:named_descendant_for_point_range(start_point, end_point)` - Get named descendant for point range.

### Inspection

- `node:kind()` - Get node type string.
- `node:type()` - Alias for `kind()`.
- `node:is_named()` - Check if named node.
- `node:is_extra()` - Check if extra node.
- `node:is_missing()` - Check if missing node.
- `node:is_error()` - Check if error node.
- `node:has_error()` - Check if contains errors.
- `node:grammar_name()` - Get grammar name.

### Position

- `node:start_byte()` - Get start byte offset.
- `node:end_byte()` - Get end byte offset.
- `node:start_point()` - Get start {row, column}.
- `node:end_point()` - Get end {row, column}.

### Content

- `node:text()` - Get source text for node.
- `node:to_sexp()` - Get S-expression representation.

## Query Methods

### query:captures(node, source)

Execute query and return captures.

Returns:
- `captures`: Array of {name, node, text, index} tables.

### query:matches(node, source)

Execute query and return matches.

Returns:
- `matches`: Array of {id, pattern, captures} tables.

### query:pattern_count()

Get number of patterns.

### query:capture_count()

Get number of captures.

### query:string_count()

Get number of strings in the query.

### query:start_byte_for_pattern(pattern_index)

Get start byte offset for a pattern.

### query:end_byte_for_pattern(pattern_index)

Get end byte offset for a pattern.

### query:set_byte_range(start_byte, end_byte)

Set byte range for query cursor.

### query:set_point_range(start_point, end_point)

Set point range for query cursor.

### query:set_match_limit(limit)

Set maximum number of matches.

### query:get_match_limit()

Get current match limit.

### query:did_exceed_match_limit()

Check if match limit was exceeded.

### query:set_timeout(microseconds)

Set query timeout.

### query:get_timeout()

Get current timeout.

### query:disable_pattern(pattern_index)

Disable a pattern by index.

### query:disable_capture(name)

Disable a capture by name.

### query:is_pattern_rooted(pattern_index)

Check if pattern is rooted.

### query:is_pattern_non_local(pattern_index)

Check if pattern is non-local.

### query:capture_name_for_id(id)

Get capture name for ID.

### query:capture_index_for_name(name)

Get capture index for name.

### query:capture_quantifier(pattern_index, capture_id)

Get capture quantifier.

### query:set_max_start_depth(depth)

Set maximum start depth.

### query:get_property_predicates(pattern_index)

Get property predicates for pattern.

### query:get_property_settings(pattern_index)

Get property settings for pattern.

### query:is_pattern_guaranteed(byte_offset)

Check if pattern is guaranteed at byte offset.

### query:get_text_predicates(pattern_index)

Get text predicates for pattern.

### query:close()

Close and release the query.

## Cursor Methods

### cursor:current_node()

Get current node.

### cursor:current_depth()

Get current depth.

### cursor:current_field_id()

Get current field ID.

### cursor:current_field_name()

Get current field name.

### cursor:current_descendant_index()

Get current descendant index.

### cursor:goto_parent()

Move to parent. Returns success boolean.

### cursor:goto_first_child()

Move to first child. Returns success boolean.

### cursor:goto_last_child()

Move to last child. Returns success boolean.

### cursor:goto_next_sibling()

Move to next sibling. Returns success boolean.

### cursor:goto_previous_sibling()

Move to previous sibling. Returns success boolean.

### cursor:goto_descendant(index)

Move to descendant by index.

### cursor:goto_first_child_for_byte(byte)

Move to first child containing byte.

### cursor:goto_first_child_for_point(row, column)

Move to first child containing point.

### cursor:reset(node)

Reset cursor to node.

### cursor:reset_to(node)

Reset cursor to a specific node.

### cursor:copy()

Create a copy of the cursor.

### cursor:close()

Close and release the cursor.

## Language Methods

### language:version()

Get language ABI version.

### language:node_kind_count()

Get number of node types.

### language:parse_state_count()

Get number of parse states.

### language:node_kind_for_id(id)

Get node type name for ID.

### language:id_for_node_kind(name, named)

Get ID for node type name.

### language:node_kind_is_named(id)

Check if node kind is named.

### language:field_count()

Get number of field names.

### language:field_name_for_id(id)

Get field name for ID.

### language:field_id_for_name(name)

Get ID for field name.

## Error Handling

### Error Types

1. **Invalid Language:**

```lua
local tree, err = treesitter.parse("invalid", "code")
-- err:kind() == errors.INVALID
-- err:retryable() == false
```

2. **Invalid Query Pattern:**

```lua
local query, err = treesitter.query("go", "((invalid")
-- err:kind() == errors.INVALID
-- err:retryable() == false
```

### Error Kind Comparison

Always use `errors.*` constants:

```lua
if err:kind() == errors.INVALID then
    -- handle invalid input
end
```

## Example Usage

```lua
local treesitter = require("treesitter")

-- Parse Go code
local code = [[
package main

func hello() {
    println("Hello")
}
]]

local tree = treesitter.parse("go", code)
local root = tree:root_node()

-- Query for function names
local query = treesitter.query("go", [[
    (function_declaration name: (identifier) @name)
]])

local captures = query:captures(root, code)
for _, cap in ipairs(captures) do
    print(cap.text)  -- "hello"
end

-- Traverse with cursor
local cursor = tree:walk()
while cursor:goto_first_child() do
    local node = cursor:current_node()
    print(node:kind())
end
cursor:close()
```

## Supported Languages

Check supported languages at runtime:

```lua
local langs = treesitter.supported_languages()
if langs["go"] then
    -- Go parsing is available
end
```

Common languages include: `go`, `lua`, `javascript`, `typescript`, `python`, `rust`, `c`, `cpp`, `json`, `yaml`, `html`, `css`, `sql`.

## Thread Safety

- Module tables are immutable and shared.
- Parser, Tree, Query, and Cursor objects are not thread-safe.
- Create new objects per-use for isolation.

## Module Classification

- **Class**: `encoding`, `deterministic`

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "treesitter",
    Description: "Tree-sitter parsing and syntax analysis",
    Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
    Build:       buildModule,
}
```
