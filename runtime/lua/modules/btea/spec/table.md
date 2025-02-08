Below is an updated specification that now incorporates style support for table widgets, along with an updated demo that shows how to create and configure a table with custom styles.

---

# Bubble Tea Table Widget in Lua

## Overview

The table widget provides a way to display tabular data with customizable columns, rows, cursor navigation, and styles. The widget supports basic interaction (moving the selection, scrolling, etc.) and is fully configurable through Lua. Under the hood it leverages a Bubble Tea model, so you can update and render it in your application.

## Table Creation

A table widget is created with the `btea.table` constructor. The constructor accepts a Lua table of options. The supported options include:

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
    header   = header_style,   -- see Style section below
    cell     = cell_style,
    selected = selected_style,
  },
}
```

## Style Creation

Styles are created with `btea.style()`. The style binding allows you to chain transformations such as `:bold()`, `:padding()`, `:foreground()`, and `:background()`. For example:

```lua
local header_style = btea.style()
    :bold()
    :padding(0, 1)
    :foreground("#FFFFFF")
    :background("#7D56F4")

local cell_style = btea.style()
    :padding(0, 1)
    :foreground("#C0CAF5")
    :background("#1E1E2E")

local selected_style = btea.style()
    :bold(true)
    :foreground("#89B4FA")
    :background("#2E2E3E")
```

## Methods

### Content Management

- **Setting Rows and Columns**
    - `tablewidget:set_rows(rows)`  
      _Rows_ is a Lua table where each element is a table of strings.
    - `tablewidget:set_columns(cols)`  
      _Columns_ is a Lua table where each element is a table with keys `"title"` and `"width"`.

- **Retrieving Data**
    - `local rows = tablewidget:get_rows()`
    - `local cols = tablewidget:get_columns()`

### Navigation and Selection

- **Cursor Control**
    - `tablewidget:set_cursor(n)`  
      Sets the selected row index (zero-based).
    - `local idx = tablewidget:cursor()`  
      Retrieves the current cursor index.

- **Movement**
    - `tablewidget:move_up([n])`  
      Moves the selection up by _n_ rows (default is 1).
    - `tablewidget:move_down([n])`  
      Moves the selection down by _n_ rows (default is 1).
    - `tablewidget:goto_top()`  
      Moves the selection to the first row.
    - `tablewidget:goto_bottom()`  
      Moves the selection to the last row.
    - `local row = tablewidget:selected_row()`  
      Returns the currently selected row as a Lua table.

### Focus Management

- **Focusing and Blurring**
    - `tablewidget:focus()`  
      Puts the table in focus so that user input will affect it.
    - `tablewidget:blur()`  
      Removes focus from the table, preventing selection movement.

### Rendering

- **Display and Help**
    - `local s = tablewidget:view()`  
      Returns the current rendered view of the table (headers and visible rows).
    - `local help = tablewidget:help_view()`  
      Returns a help text showing the key bindings.

### Dimension Configuration

- **Viewport Adjustments**
    - `tablewidget:set_width(n)`  
      Sets the viewport width.
    - `tablewidget:set_height(n)`  
      Sets the viewport height.
    - `local width = tablewidget:width()`
    - `local height = tablewidget:height()`

### Data Parsing

- **Creating Rows from a String**
    - `tablewidget:from_values(value, [separator])`  
      Parses a multi-line string into rows and columns.
        - _value_: the string containing the data (rows separated by newline).
        - _separator_: the field delimiter (default is comma if not provided).