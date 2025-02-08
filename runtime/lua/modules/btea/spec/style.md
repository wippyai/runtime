# Lip Gloss Style Binding in Lua

## Overview

This specification defines how Lip Gloss style objects are represented and manipulated in Lua. Each style object is a
userdata that encapsulates configuration for terminal text rendering, including colors, formatting, and layout
properties. All style modification methods are immutable – they return a new style object without altering the original.

## Style Object Structure

A style object is created via the constructor:

```lua
local style = btea.style()
```

Internally, the style object provides a set of methods that mirror the Lip Gloss API, enabling expressive, chainable
style modifications.

## Available Methods

Each style object supports the following methods:

### render(string)

Renders the given string using the style's configuration.

```lua
local styledText = style:render("Hello, world!")
```

### foreground(color)

Sets the foreground (text) color. Accepts ANSI codes or hexadecimal color strings.

```lua
local newStyle = style:foreground("#FAFAFA")
```

### background(color)

Sets the background color.

```lua
local newStyle = style:background("#7D56F4")
```

### bold()

Enables bold text.

```lua
local newStyle = style:bold()
```

### italic()

Enables italic text.

```lua
local newStyle = style:italic()
```

### underline()

Enables underlined text.

```lua
local newStyle = style:underline()
```

### strikethrough()

Enables strikethrough formatting.

```lua
local newStyle = style:strikethrough()
```

### faint()

Enables faint text styling.

```lua
local newStyle = style:faint()
```

### blink()

Enables blinking text.

```lua
local newStyle = style:blink()
```

### reverse()

Enables reverse video mode.

```lua
local newStyle = style:reverse()
```

### padding(top, right, bottom, left)

Sets the padding around the rendered text. The function accepts one to four numerical arguments, following CSS shorthand
conventions:

- `padding(2)` sets uniform padding.
- `padding(2, 4)` sets vertical and horizontal padding.
- `padding(1, 4, 2)` sets top, horizontal, and bottom.
- `padding(2, 4, 3, 1)` sets top, right, bottom, and left respectively.

```lua
local newStyle = style:padding(2, 4, 2, 4)
```

### margin(top, right, bottom, left)

Sets the margin around the rendered text. It follows the same shorthand as `padding`.

```lua
local newStyle = style:margin(1, 2, 1, 2)
```

### border(borderStyle)

Sets the border style using one of the predefined border types. Valid values for `borderStyle` are:

- `"normal"`
- `"rounded"`
- `"thick"`
- `"double"`

```lua
local newStyle = style:border("rounded")
```

### custom_border(borderTable)

Allows the user to specify a custom border via a Lua table. The table may include any of the following keys to define
individual border segments (all keys are optional):

- `top`
- `bottom`
- `left`
- `right`
- `top_left`
- `top_right`
- `bottom_left`
- `bottom_right`

```lua
local custom = {
  top = "─",
  bottom = "─",
  left = "│",
  right = "│",
  top_left = "┌",
  top_right = "┐",
  bottom_left = "└",
  bottom_right = "┘",
}

local newStyle = style:custom_border(custom)
```

### width(number)

Sets the minimum width (in cells) for the rendered output.

```lua
local newStyle = style:width(24)
```

### height(number)

Sets the minimum height (in cells) for the rendered output.

```lua
local newStyle = style:height(32)
```

### align(alignment)

Sets text alignment within the available width. Use the provided alignment constants:

- `align.LEFT`
- `align.CENTER`
- `align.RIGHT`

```lua
local newStyle = style:align(align.CENTER)
```

### inline(boolean)

Forces the style to render on a single line, ignoring margins, padding, and borders.

```lua
local newStyle = style:inline(true)
```

### max_width(number)

Constrains the rendered output to a maximum width.

```lua
local newStyle = style:max_width(80)
```

### max_height(number)

Constrains the rendered output to a maximum height.

```lua
local newStyle = style:max_height(10)
```

### tab_width(number)

Sets the number of spaces for converting tab characters. A value of 0 removes tabs entirely, while a special constant (
e.g., `lipgloss.NoTabConversion`) can leave tabs intact.

```lua
local newStyle = style:tab_width(4)
```

### copy()

Creates and returns a deep copy of the style object.

```lua
local copyStyle = style:copy()
```

### inherit(otherStyle)

Inherits unset properties from another style object, combining configurations.

```lua
local combinedStyle = style:inherit(otherStyle)
```

## Constants

The binding provides constants for alignment:

```lua
align = {
  LEFT = 0,
  CENTER = 1,
  RIGHT = 2,
}
```

Predefined border style strings are also provided:

- `"normal"`
- `"rounded"`
- `"thick"`
- `"double"`

## Best Practices

1. **Immutable Operations:** Each method returns a new style object. Use the returned object for further modifications.
2. **Method Chaining:** Since operations are immutable, you can chain methods for concise configuration:

    ```lua
    local styled = lipgloss.new_style()
      :foreground("#FAFAFA")
      :background("#7D56F4")
      :bold()
      :padding(2, 4)
      :width(22)
    ```
3. **Separation of Concerns:** Define your styles separately from your rendering logic to maintain clean, modular code.

## Error Handling

- **Invalid Inputs:** Passing invalid color strings, border style values, or incorrect keys for custom borders may
  trigger runtime errors. Validate inputs where necessary.
- **Alignment Values:** Use only the provided alignment constants to ensure expected behavior.

## Example Usage

```lua
local style = lipgloss.new_style()
  :foreground("#FAFAFA")
  :background("#7D56F4")
  :bold()
  :padding(2, 4)
  :width(22)

local customBorder = {
  top = "─",
  bottom = "─",
  left = "│",
  right = "│",
  topleft = "┌",
  topright = "┐",
  bottomleft = "└",
  bottomright = "┘",
}

local styledWithBorder = style:custom_border(customBorder)
print(styledWithBorder:render("Hello, kitty"))
```

In this example, a style is created with bold text, specified foreground and background colors, padding, and a set
width, then a custom border is applied and used to render a string.

## Conclusion

This specification outlines the available methods and best practices for working with Lip Gloss style objects in Lua. By
following these guidelines, developers can effectively manage and apply terminal styling in their Lua applications,
including the ability to define custom borders when needed.