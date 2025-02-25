# Bubble Tea Text Utilities in Lua

## Overview

This specification defines how text manipulation utilities are represented and used in Lua within the Bubble Tea
framework. These utilities provide functions for measuring text dimensions, joining text blocks, and applying styles to
specific characters.

## Module Structure

Text utilities are accessed through the `btea.text` namespace, which provides various functions grouped by purpose.

## Text Measurement

### Width Calculation

```lua
local width = btea.text.width(str)
```

Returns the cell width of characters in the string, properly handling:

- ANSI escape sequences (ignored in measurement)
- Wide characters (CJK, emojis, etc.)
- Multi-line strings (returns maximum line width)

### Height Calculation

```lua
local height = btea.text.height(str)
```

Returns the height of a string in cells by counting newline characters.

### Combined Dimension Calculation

```lua
local width, height = btea.text.size(str)
```

Returns both width and height in a single call.

### Maximum Dimension Functions

```lua
local max_width = btea.text.max_width(strings)
local max_height = btea.text.max_height(strings)
```

Takes a table of strings and returns the maximum width or height among them.

## Text Joining

### Position Constants

```lua
btea.text.position = {
    TOP = 0.0,
    CENTER = 0.5,
    BOTTOM = 1.0,
    LEFT = 0.0,
    RIGHT = 1.0
}
```

### Horizontal Joining

```lua
local result = btea.text.join_horizontal(position, str1, str2, ...)
```

Joins strings horizontally with alignment specified by position (0.0 - 1.0):

- 0.0 (TOP): Align at the top
- 0.5 (CENTER): Center vertically
- 1.0 (BOTTOM): Align at the bottom

Example:

```lua
local top_aligned = btea.text.join_horizontal(btea.text.position.TOP,
    "Block 1\nLine 2",
    "Block 2\nLine 2\nLine 3"
)
```

### Vertical Joining

```lua
local result = btea.text.join_vertical(position, str1, str2, ...)
```

Joins strings vertically with alignment specified by position (0.0 - 1.0):

- 0.0 (LEFT): Align to the left
- 0.5 (CENTER): Center horizontally
- 1.0 (RIGHT): Align to the right

Example:

```lua
local right_aligned = btea.text.join_vertical(btea.text.position.RIGHT,
    "Short line",
    "This is a longer line"
)
```

## Character Styling

### Style Specific Characters

```lua
local result = btea.text.style_runes(str, indices, matched_style, unmatched_style)
```

Applies different styles to specific characters in a string:

- `str`: Input string to style
- `indices`: Table of 0-based indices for characters to style
- `matched_style`: Style to apply to characters at specified indices
- `unmatched_style`: Style to apply to all other characters

Example:

```lua
local matched = btea.style():foreground("red")
local unmatched = btea.style():foreground("blue")

local result = btea.text.style_runes(
    "Hello World",
    {0, 4, 6},  -- Style 'H', 'o', 'W'
    matched,
    unmatched
)
```

## Best Practices

1. **Width Calculation**
    - Use `width()` instead of string length for proper character width handling
    - Account for zero-width ANSI sequences in styled text

2. **Text Joining**
    - Use position constants for common alignments
    - Use decimal positions (0-1) for fine-tuned control
    - Consider text block dimensions when choosing alignment

3. **Style Application**
    - Apply contrasting styles for better visibility
    - Validate character indices before styling
    - Consider terminal color support

## Error Handling

1. **Invalid Input**
    - Invalid position values (outside 0-1) may produce unexpected results
    - Invalid indices in style_runes are ignored
    - Non-string inputs will raise errors

2. **Style Objects**
    - Invalid style objects will raise type errors
    - Nil style objects are not allowed

## Text Sanitization

### Sanitize Control Characters

```lua
local clean = btea.text.sanitize_runes(str [, newline_repl [, tab_repl]])
```

Processes input string to handle control characters:

- Removes invalid UTF-8 sequences
- Removes control characters
- Optionally replaces newlines and tabs with custom strings
- Preserves all other valid characters

Parameters:

- `str`: Input string to sanitize
- `newline_repl`: (optional) String to replace newlines with, defaults to "\n"
- `tab_repl`: (optional) String to replace tabs with, defaults to 4 spaces

Example:

```lua
-- Basic usage - remove control chars
local text = btea.text.sanitize_runes("some\x00text\nwith\tcontrol\rchars")

-- Custom replacements
local html = btea.text.sanitize_runes(
    "Line 1\nLine 2\tIndented",
    "<br>",     -- Replace newlines with HTML breaks
    "&nbsp;&nbsp;"  -- Replace tabs with HTML spaces
)
```

## Example Usage

### Complex Text Layout

```lua
-- Create header with different styles
local header = btea.text.style_runes(
    "DASHBOARD",
    {0, 4, 8},
    btea.style():bold():foreground("red"),
    btea.style():foreground("blue")
)

-- Create two columns
local left_column = "Status: Active\nUsers: 150\nLoad: 75%"
local right_column = "CPU: 45%\nMem: 2.5GB\nDisk: 80%"

-- Join columns horizontally
local stats = btea.text.join_horizontal(
    btea.text.position.TOP,
    left_column,
    right_column
)

-- Join header and stats vertically
local dashboard = btea.text.join_vertical(
    btea.text.position.CENTER,
    header,
    stats
)
```

### Dynamic Width Handling

```lua
local items = {
    "Item 1",
    "A longer item 2",
    "Very long item 3"
}

-- Get maximum width for layout
local width = btea.text.max_width(items)

-- Create uniform width items
local formatted = {}
for _, item in ipairs(items) do
    local style = btea.style():width(width):padding(0, 1)
    table.insert(formatted, style:render(item))
end
```

## Notes

- Text measurement accounts for terminal-specific character widths
- Join operations preserve ANSI sequences and styling
- Style application works with multi-byte characters
- All operations are non-destructive and return new strings