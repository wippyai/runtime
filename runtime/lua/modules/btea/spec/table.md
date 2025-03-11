# Bubble Tea Table Widget in Lua

## Overview

The table widget provides a way to display tabular data with customizable columns, rows, cursor navigation, and styles.
The widget supports basic interaction (moving the selection, scrolling, etc.) and is fully configurable through Lua.
Under the hood it leverages a Bubble Tea model, so you can update and render it in your application.

## Table Creation

A table widget is created with the `btea.table` constructor. The constructor accepts a Lua table of options. The
supported options include:

- **cols**: A list of column definitions. Each column is represented by a Lua table with:
    - `title` (string): The header text.
    - `width` (number): The column width.
- **rows**: A list of rows. Each row is a Lua table (array) of strings.
- **width**: (number) Sets the overall viewport width.
- **height**: (number) Sets the viewport height (excluding header height).
- **focused**: (boolean) Determines whether the table is in focus (enabling selection/movement).
- **styles**: A Lua table defining the styles for different table parts. It should include:
    - `header`: A btea.Style instance to style the header row.
    - `cell`: A btea.Style instance to style each cell.
    - `selected`: A btea.Style instance for the selected row.

### Example:

```lua
local tablewidget = btea.table {
  cols = {
    { title = "ID", width = 10 },
    { title = "Name", width = 20 },
    { title = "Status", width = 15 },
  },
  rows = {
    {"1", "Alice", "Active"},
    {"2", "Bob", "Inactive"},
    {"3", "Carol", "Active"},
  },
  width = 60,
  height = 20,
  focused = true,
  styles = {
    header   = btea.style():bold():foreground("#FFFFFF"):background("#7D56F4"),
    cell     = btea.style():foreground("#C0CAF5"),
    selected = btea.style():bold():foreground("#89B4FA"):background("#2E2E3E"),
  },
}
```

## Style Creation

Styles are created with `btea.style()`. The style binding allows you to chain transformations such as `:bold()`,
`:foreground()`, and `:background()`. For example:

```lua
local header_style = btea.style()
    :bold()
    :foreground("#FFFFFF")
    :background("#7D56F4")

local cell_style = btea.style()
    :foreground("#C0CAF5")

local selected_style = btea.style()
    :bold()
    :foreground("#89B4FA")
    :background("#2E2E3E")
```

## Methods

### Content Management

- **Setting Rows and Columns**
    - `tablewidget:set_rows(rows)`  
      Sets the table rows. _rows_ must be a Lua table where each element is a table of strings.
    - `tablewidget:set_columns(cols)`  
      Sets the table columns. _cols_ must be a Lua table where each element is a table with keys `"title"` and
      `"width"`.

- **Retrieving Data**
    - `local rows = tablewidget:get_rows()`  
      Returns the current rows as a Lua table of tables.
    - `local cols = tablewidget:get_columns()`  
      Returns the current columns as a Lua table of column definitions.

### Navigation and Selection

- **Cursor Control**
    - `tablewidget:set_cursor(n)`  
      Sets the selected row index (zero-based). The cursor will be clamped to valid row indices.
    - `local idx = tablewidget:cursor()`  
      Returns the current cursor index (zero-based).

- **Movement**
    - `tablewidget:move_up([n])`  
      Moves the selection up by _n_ rows (default is 1). Won't move past the first row.
    - `tablewidget:move_down([n])`  
      Moves the selection down by _n_ rows (default is 1). Won't move past the last row.
    - `tablewidget:goto_top()`  
      Moves the selection to the first row (index 0).
    - `tablewidget:goto_bottom()`  
      Moves the selection to the last row.
    - `local row = tablewidget:selected_row()`  
      Returns the currently selected row as a Lua table, or nil if no row is selected.

### Focus Management

- **Focusing and Blurring**
    - `tablewidget:focus()`  
      Puts the table in focus for user input handling.
    - `tablewidget:blur()`  
      Removes focus from the table.

### Rendering

- **Display and Help**
    - `local s = tablewidget:view()`  
      Returns the current rendered view of the table.
    - `local help = tablewidget:help_view()`  
      Returns a help text showing available key bindings.

### Dimension Configuration

- **Viewport Adjustments**
    - `tablewidget:set_width(n)`  
      Sets the viewport width. Must be called after table creation if not set in options.
    - `tablewidget:set_height(n)`  
      Sets the viewport height. Must be called after table creation if not set in options.
    - `local width = tablewidget:width()`  
      Returns the current viewport width.
    - `local height = tablewidget:height()`  
      Returns the current viewport height.

### Data Parsing

- **Creating Rows from a String**
    - `tablewidget:from_values(value, [separator])`  
      Parses a multi-line string into rows. Each line becomes a row, split by the separator.
        - _value_: String containing the data (rows separated by newline)
        - _separator_: Field delimiter (defaults to comma if not provided)

### Update Handling

The table widget implements the Bubble Tea update pattern:

```lua
function on_update(msg)
    local cmd = tablewidget:update(msg)
    -- Handle any returned command if needed
end
```