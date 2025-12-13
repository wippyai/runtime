# treesitter

Tree-sitter parsing and syntax analysis for multiple programming languages. Encoding, deterministic.

## Loading

```lua
local treesitter = require("treesitter")
```

## Functions

### supported_languages() → table

Returns table of supported programming languages.

**Returns:** `table` - map of language names to `true` (e.g., `{go = true, javascript = true, ...}`)

### language(name: string) → Language, error

Creates a Language object for the specified language.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Language name or alias (e.g., "go", "js", "typescript") |

**Returns:**
- Success: `Language` - language object
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| language not supported | errors.INVALID | no |
| language has no binding | errors.INVALID | no |

### parse(language: string, code: string) → Tree, error

Parses source code into a syntax tree.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| language | string | yes | - | Language name or alias |
| code | string | yes | - | Source code to parse |

**Returns:**
- Success: `Tree` - syntax tree
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| language not supported | errors.INVALID | no |
| language has no binding | errors.INVALID | no |
| parse failed | errors.INTERNAL | no |
| no context found | errors.INTERNAL | no |

### parser() → Parser, error

Creates a new Parser object.

**Returns:**
- Success: `Parser` - parser object
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context found | errors.INTERNAL | no |

### query(language: string, pattern: string) → Query, error

Creates a query from a tree-sitter query pattern.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| language | string | yes | - | Language name or alias |
| pattern | string | yes | - | Tree-sitter query pattern (S-expression syntax) |

**Returns:**
- Success: `Query` - query object
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| language not supported | errors.INVALID | no |
| language has no binding | errors.INVALID | no |
| invalid query pattern | errors.INVALID | no |
| no context found | errors.INTERNAL | no |

## Types

### Parser

Returned by `treesitter.parser()`. Must call `:set_language()` before use.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| set_language | (language: string) | boolean, error | sets parser language |
| get_language | () | string, error | returns current language name |
| parse | (code: string, old_tree?: Tree) | Tree, error | parses code, optionally reusing old tree |
| reset | () | - | resets parser state |
| set_timeout | (seconds: number) | - | sets parse timeout in seconds |
| set_ranges | (ranges: table[]) | boolean, error | sets included byte ranges for parsing |
| close | () | - | releases parser resources |

#### parser:set_language(language: string) → boolean, error

Sets the parser's language.

**Returns:**
- Success: `true`
- Error: `false, error` - structured error with kind `errors.INVALID` or `errors.INTERNAL`

#### parser:get_language() → string, error

Returns the currently set language name.

**Returns:**
- Success: `string` - language name
- Error: `nil, error` - structured error with kind `errors.INVALID` if language not set

#### parser:parse(code: string, old_tree?: Tree) → Tree, error

Parses source code into a syntax tree.

**Returns:**
- Success: `Tree` - parsed syntax tree
- Error: `nil, error` - structured error with kind `errors.INVALID` or `errors.INTERNAL`

**Errors:** Returns error if language not set, parse fails, or no context found

#### parser:set_ranges(ranges: table[]) → boolean, error

Sets byte ranges to include in parsing.

**ranges table structure:**

Each range must have:
- `start_byte`: integer - start byte offset
- `end_byte`: integer - end byte offset
- `start_row`: integer - start row position
- `start_col`: integer - start column position
- `end_row`: integer - end row position
- `end_col`: integer - end column position

**Returns:**
- Success: `true`
- Error: `false, error` - structured error

### Tree

Returned by `treesitter.parse()` or `parser:parse()`. Represents a parsed syntax tree.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| root_node | () | Node, error | returns root node of tree |
| root_node_with_offset | (offset_bytes: integer, offset_point: table) | Node, error | returns root with offset applied |
| language | () | Language, error | returns tree's language object |
| copy | () | Tree, error | creates deep copy of tree |
| walk | () | Cursor, error | creates cursor for tree traversal |
| edit | (edit: table) | boolean, error | applies edit to tree |
| changed_ranges | (other_tree: Tree) | table[] | returns ranges that changed between trees |
| included_ranges | () | table[] | returns included ranges set on parser |
| dot_graph | () | string, error | returns DOT graph representation |
| close | () | - | releases tree resources |

#### tree:root_node() → Node, error

Returns the root node of the syntax tree.

**Returns:**
- Success: `Node` - root node
- Error: `nil, error` - structured error if tree is closed

#### tree:root_node_with_offset(offset_bytes: integer, offset_point: table) → Node, error

Returns root node with byte and point offset applied.

**offset_point table:**
- `row`: integer
- `column`: integer

**Returns:**
- Success: `Node` - root node with offset
- Error: `nil, error` - structured error if tree is closed

#### tree:edit(edit: table) → boolean, error

Applies an incremental edit to the tree.

**edit table structure:**

Required fields:
- `start_byte`: number - byte offset where edit starts
- `old_end_byte`: number - byte offset where old content ends
- `new_end_byte`: number - byte offset where new content ends
- `start_row`: number - row where edit starts
- `start_column`: number - column where edit starts
- `old_end_row`: number - row where old content ends
- `old_end_column`: number - column where old content ends
- `new_end_row`: number - row where new content ends
- `new_end_column`: number - column where new content ends

**Returns:**
- Success: `true`
- Error: `false, error` - structured error with kind `errors.INVALID` for invalid positions

#### tree:changed_ranges(other_tree: Tree) → table[]

Returns ranges that changed between this tree and another tree.

**Returns:** Array of range tables, each with:
- `start_point`: table with `row` and `column`
- `end_point`: table with `row` and `column`
- `start_byte`: number
- `end_byte`: number

#### tree:included_ranges() → table[]

Returns the ranges that were included during parsing.

**Returns:** Array of range tables (same structure as `changed_ranges`)

### Node

Returned by tree and node navigation methods. Represents a syntax tree node.

**Navigation methods:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| parent | () | Node \| nil | parent node or nil if root |
| child | (index: integer) | Node \| nil | child at index (0-based) |
| child_count | () | integer | total number of children |
| named_child | (index: integer) | Node \| nil | named child at index (0-based) |
| named_child_count | () | integer | number of named children |
| next_sibling | () | Node \| nil | next sibling or nil |
| prev_sibling | () | Node \| nil | previous sibling or nil |
| next_named_sibling | () | Node \| nil | next named sibling or nil |
| prev_named_sibling | () | Node \| nil | previous named sibling or nil |
| descendant_count | () | integer | total descendant count |
| named_descendant_for_point_range | (start_point: table, end_point: table) | Node \| nil | named descendant in range |

**Field methods:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| child_by_field_name | (name: string) | Node \| nil | child with field name |
| field_name_for_child | (index: integer) | string \| nil | field name for child at index |

**Inspection methods:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| kind | () | string | node type (e.g., "identifier", "function_declaration") |
| type | () | string | alias for kind() |
| grammar_name | () | string | grammar name for this node |
| is_named | () | boolean | true if named node |
| is_extra | () | boolean | true if extra node |
| is_missing | () | boolean | true if node represents missing syntax |
| has_error | () | boolean | true if node or descendants have errors |
| is_error | () | boolean | true if node is error node |

**Position methods:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| start_byte | () | integer | byte offset where node starts |
| end_byte | () | integer | byte offset where node ends |
| start_point | () | table | point with `row` and `column` |
| end_point | () | table | point with `row` and `column` |

**Content methods:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| text | (source?: string) | string, error | node's source text |
| to_sexp | () | string | S-expression representation |

#### node:named_descendant_for_point_range(start_point: table, end_point: table) → Node | nil

Finds smallest named descendant spanning the point range.

**point table structure:**
- `row`: integer
- `column`: integer

**Returns:** `Node` or `nil` if not found

#### node:text(source?: string) → string, error

Returns the source text for this node.

**Parameters:**
- `source` (optional): If provided, extracts text from this string. Otherwise uses tree's source.

**Returns:**
- Success: `string` - node's text content
- Error: `nil, error` - structured error with kind `errors.INVALID` if source reference empty or byte range invalid

### Query

Returned by `treesitter.query()`. Executes tree-sitter queries on syntax trees.

**Query execution:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| matches | (node: Node, source: string) | table[] | returns all matches |
| captures | (node: Node, source: string) | table[] | returns all captures |

**Query information:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| pattern_count | () | integer | number of patterns in query |
| capture_count | () | integer | number of captures defined |
| capture_name_for_id | (id: integer) | string \| nil | capture name for ID |
| capture_index_for_name | (name: string) | integer \| nil | ID for capture name |
| start_byte_for_pattern | (pattern: integer) | integer | start byte of pattern |
| end_byte_for_pattern | (pattern: integer) | integer | end byte of pattern |

**Query configuration:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| set_byte_range | (start: integer, end: integer) | - | limits query to byte range |
| set_point_range | (start_point: table, end_point: table) | - | limits query to point range |
| set_match_limit | (limit: integer) | - | sets max matches |
| get_match_limit | () | integer | returns match limit |
| did_exceed_match_limit | () | boolean | true if limit exceeded |
| set_timeout | (micros: integer) | - | sets timeout in microseconds |
| get_timeout | () | integer | returns timeout in microseconds |
| set_max_start_depth | (depth: integer) | - | sets max traversal depth |

**Pattern control:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| disable_pattern | (pattern: integer) | - | disables pattern by index |
| disable_capture | (name: string) | - | disables capture by name |
| is_pattern_rooted | (pattern: integer) | boolean | true if pattern is rooted |
| is_pattern_non_local | (pattern: integer) | boolean | true if pattern is non-local |
| is_pattern_guaranteed | (byte_offset: integer) | boolean | true if pattern guaranteed at offset |
| capture_quantifier | (pattern: integer, id: integer) | number \| nil | quantifier for capture |

**Predicate methods:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| get_property_predicates | (pattern: integer) | table[] | property predicates for pattern |
| get_property_settings | (pattern: integer) | table[] | property settings for pattern |
| get_text_predicates | (pattern: integer) | table[] | text predicates for pattern |

**Resource management:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| close | () | - | releases query resources |

#### query:matches(node: Node, source: string) → table[]

Executes query and returns all matches.

**Returns:** Array of match tables, each containing:
- `id`: integer - match ID
- `pattern`: integer - pattern index
- `captures`: table[] - array of captures, each with:
  - `node`: Node - captured node
  - `index`: integer - capture index
  - `name`: string - capture name

**Errors:**

Returns `nil, error` with kind `errors.INVALID` if node is not a Node type.

#### query:captures(node: Node, source: string) → table[]

Executes query and returns all captures.

**Returns:** Array of capture tables, each containing:
- `node`: Node - captured node
- `index`: integer - capture index
- `name`: string - capture name
- `text`: string - captured text

**Errors:**

Returns `nil, error` with kind `errors.INVALID` if node is not a Node type.

#### query:get_property_predicates(pattern: integer) → table[]

Returns property predicates for a pattern.

**Returns:** Array of predicate tables, each with:
- `key`: string - property key
- `value`: string (optional) - property value
- `capture_id`: integer (optional) - associated capture ID
- `positive`: boolean - true if positive predicate

#### query:get_property_settings(pattern: integer) → table[]

Returns property settings for a pattern.

**Returns:** Array of setting tables, each with:
- `key`: string - setting key
- `value`: string (optional) - setting value
- `capture_id`: integer (optional) - associated capture ID

#### query:get_text_predicates(pattern: integer) → table[]

Returns text predicates for a pattern.

**Returns:** Array of predicate tables, each with:
- `type`: integer - predicate type
- `capture_id`: integer - capture ID
- `positive`: boolean - true if positive predicate
- `match_all_nodes`: boolean - true if matches all nodes
- `value`: varies - predicate value (integer, string, regex, or string array)

### Cursor

Returned by `tree:walk()`. Provides efficient tree traversal.

**Current state:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| current_node | () | Node \| nil | node at cursor position |
| current_field_id | () | integer | field ID at cursor |
| current_field_name | () | string \| nil | field name at cursor |
| current_depth | () | integer | depth in tree (0 = root) |
| current_descendant_index | () | integer | descendant index |

**Navigation:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| goto_parent | () | boolean | moves to parent, returns success |
| goto_first_child | () | boolean | moves to first child, returns success |
| goto_last_child | () | boolean | moves to last child, returns success |
| goto_next_sibling | () | boolean | moves to next sibling, returns success |
| goto_previous_sibling | () | boolean | moves to previous sibling, returns success |
| goto_descendant | (index: integer) | - | moves to descendant by index |
| goto_first_child_for_byte | (byte: integer) | integer \| nil | moves to first child containing byte |
| goto_first_child_for_point | (point: table) | integer \| nil | moves to first child containing point |

**Cursor management:**

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| reset | (node: Node) | - | resets cursor to node |
| reset_to | (cursor: Cursor) | - | resets to match another cursor |
| copy | () | Cursor, error | creates copy of cursor |
| close | () | - | releases cursor resources |

#### cursor:goto_first_child_for_point(point: table) → integer | nil

Moves cursor to first child containing the point.

**point table:**
- `row`: integer
- `column`: integer

**Returns:** Child index if found, `nil` otherwise

### Language

Returned by `treesitter.language()`. Provides language metadata.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| version | () | integer | tree-sitter ABI version |
| node_kind_count | () | integer | number of node kinds |
| parse_state_count | () | integer | number of parse states |
| node_kind_for_id | (id: integer) | string | node kind name for ID |
| id_for_node_kind | (kind: string, named: boolean) | integer | ID for node kind |
| node_kind_is_named | (id: integer) | boolean | true if node kind is named |
| field_count | () | integer | number of fields |
| field_name_for_id | (id: integer) | string | field name for ID |
| field_id_for_name | (name: string) | integer | ID for field name |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local tree, err = treesitter.parse("invalid", "code")
if err then
    if err:kind() == errors.INVALID then
        -- invalid language or pattern
    elseif err:kind() == errors.INTERNAL then
        -- internal error (parse failed, no context)
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local treesitter = require("treesitter")

-- Parse Go code
local code = [[
package main

func hello() string {
    return "Hello, World!"
}

func main() {
    hello()
}
]]

local tree, err = treesitter.parse("go", code)
if err then error(err) end

-- Get root node
local root = tree:root_node()
print(root:kind())  -- "source_file"

-- Query for functions
local query, err = treesitter.query("go", [[
    (function_declaration name: (identifier) @func_name)
]])
if err then error(err) end

local captures = query:captures(root, code)
for _, capture in ipairs(captures) do
    print(capture.name, capture.text)  -- "func_name hello", "func_name main"
end

-- Use cursor for traversal
local cursor = tree:walk()
assert(cursor:goto_first_child())
local node = cursor:current_node()
print(node:kind())

-- Cleanup
cursor:close()
query:close()
tree:close()
```
