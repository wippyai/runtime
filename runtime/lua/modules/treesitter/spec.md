# Lua TreeSitter Module Specification

## Overview

The `treesitter` module provides syntax tree parsing capabilities using Tree-sitter parsers for various programming languages.

## Documentation

Detailed documentation is available in the `spec/` directory:

- [Index](spec/index.md) - Module overview and getting started
- [Parser](spec/parser.md) - Parse source code into syntax trees
- [Language](spec/language.md) - Language grammar management
- [Tree](spec/tree.md) - Syntax tree manipulation
- [Node](spec/node.md) - Working with tree nodes
- [Cursor](spec/cursor.md) - Tree traversal
- [Query](spec/query.md) - Pattern matching and queries

## Quick Example

```lua
local treesitter = require("treesitter")

-- Parse some code
local parser = treesitter.parser("lua")
local tree = parser:parse("local x = 42")

-- Get root node
local root = tree:root()
print("Root type:", root:type())

-- Traverse the tree
local cursor = root:walk()
cursor:goto_first_child()
print("First child:", cursor:current_node():type())
```

For complete API documentation, see the files in the `spec/` directory.
