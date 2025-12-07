# Lua Excel Module Specification

## Overview

The `excel` module provides Excel file operations for Lua. It allows creating, reading, and writing Excel workbooks using the `.xlsx` format.

## Module Interface

### Module Loading

```lua
local excel = require("excel")
```

### Functions

#### excel.new()

Creates a new empty Excel workbook.

Returns:

- `workbook`: Workbook object (or nil on error).
- `error`: Structured error object (or nil on success).

#### excel.open(reader: userdata)

Opens an Excel workbook from a reader object.

Parameters:

- `reader`: Object implementing `io.Reader` (e.g., file handle, buffer).

Returns:

- `workbook`: Workbook object (or nil on error).
- `error`: Structured error object (or nil on success).

## Workbook Methods

### workbook:new_sheet(name: string)

Creates a new sheet in the workbook.

Parameters:

- `name`: Name of the new sheet.

Returns:

- `index`: Sheet index number (or nil on error).
- `error`: Structured error object (or nil on success).

### workbook:get_sheet_list()

Gets a list of all sheet names in the workbook.

Returns:

- `sheets`: Array of sheet names (or nil on error).
- `error`: Structured error object (or nil on success).

### workbook:get_rows(sheet_name: string)

Gets all rows from a sheet.

Parameters:

- `sheet_name`: Name of the sheet to read.

Returns:

- `rows`: 2D array of cell values as strings (or nil on error).
- `error`: Structured error object (or nil on success).

### workbook:set_cell_value(sheet_name: string, cell: string, value: any)

Sets a cell value in the workbook.

Parameters:

- `sheet_name`: Name of the sheet.
- `cell`: Cell reference (e.g., "A1", "B2").
- `value`: Value to set (string, number, integer, boolean).

Returns:

- `error`: Structured error object (or nil on success).

### workbook:write_to(writer: userdata)

Writes the workbook to a writer object.

Parameters:

- `writer`: Object implementing `io.Writer`.

Returns:

- `error`: Structured error object (or nil on success).

### workbook:close()

Closes the workbook and releases resources.

Returns:

- `error`: Structured error object (or nil on success).

## Error Handling

The module returns structured errors using the `lua.Error` type.

### Error Types

1. **Invalid Workbook:** If the workbook object is invalid.

```lua
-- fake_wb is userdata but not a valid Workbook
local index, err = fake_wb:new_sheet("Test")
-- index: nil
-- err:kind() == errors.INVALID
-- err:retryable() == false
```

2. **Internal Error:** For operation failures.

```lua
local rows, err = wb:get_rows("NonExistentSheet")
-- rows: nil
-- err:kind() == errors.INTERNAL
-- err:retryable() == false
```

3. **Closed Workbook:** Operations on a closed workbook.

```lua
wb:close()
local sheets, err = wb:get_sheet_list()
-- sheets: nil
-- err:kind() == errors.INTERNAL
-- tostring(err) contains "workbook is closed"
```

### Error Kind Comparison

Always use `errors.*` constants for kind comparison:

```lua
local rows, err = wb:get_rows(sheet_name)
if err then
    if err:kind() == errors.INVALID then
        -- handle invalid workbook
    elseif err:kind() == errors.INTERNAL then
        -- handle operation error
    end
end
```

## Behavior

1. **Value Types**
   - Strings are stored as text.
   - Numbers (integer and float) are stored as numeric values.
   - Booleans are stored as TRUE/FALSE.
   - All values are returned as strings from `get_rows`.

2. **Cell References**
   - Use Excel-style references: "A1", "B2", "AA100", etc.
   - Column letters are uppercase.

3. **Sheet Management**
   - New workbooks have a default "Sheet1".
   - Creating a sheet with an existing name returns the existing sheet index.

4. **Resource Management**
   - Always call `close()` when done with a workbook.
   - Resources are automatically cleaned up when the Lua context ends.

## Thread Safety

- Workbook objects are single-thread owned.
- Each Lua state should use its own workbook instances.

## Module Classification

- **Class**: `io`, `encoding`
- Operations involve file I/O and data encoding/decoding.

## Example Usage

```lua
local excel = require("excel")

-- Create a new workbook
local wb, err = excel.new()
if err then
    print("Error:", err)
    return
end

-- Create a sheet
wb:new_sheet("Sales")

-- Set cell values
wb:set_cell_value("Sales", "A1", "Product")
wb:set_cell_value("Sales", "B1", "Price")
wb:set_cell_value("Sales", "A2", "Widget")
wb:set_cell_value("Sales", "B2", 29.99)
wb:set_cell_value("Sales", "A3", "Gadget")
wb:set_cell_value("Sales", "B3", 49.99)

-- Read rows back
local rows, err = wb:get_rows("Sales")
if err then
    print("Error:", err)
else
    for i, row in ipairs(rows) do
        print("Row " .. i .. ":", table.concat(row, ", "))
    end
end
-- Output:
-- Row 1: Product, Price
-- Row 2: Widget, 29.99
-- Row 3: Gadget, 49.99

-- Get sheet list
local sheets = wb:get_sheet_list()
for _, name in ipairs(sheets) do
    print("Sheet:", name)
end

-- Write to file (using fs module)
local fs = require("fs")
local file = fs.create("report.xlsx")
wb:write_to(file)
file:close()

-- Close workbook
wb:close()

-- Open existing file
local file = fs.open("report.xlsx")
local wb2 = excel.open(file)
local rows = wb2:get_rows("Sales")
wb2:close()
file:close()
```

## Implementation Notes

- Uses Go's `github.com/xuri/excelize/v2` library.
- Module uses `ModuleDef` struct for definition.
- Module table is created once and shared across all Lua states.
- Workbook type is registered as "excel.Workbook".
- Errors include Lua stack traces for debugging.

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "excel",
    Description: "Excel file operations",
    Class:       []string{luaapi.ClassIO, luaapi.ClassEncoding},
    Build: func() (*lua.LTable, []luaapi.YieldType) {
        mod := lua.CreateTable(0, 2)
        mod.RawSetString("new", lua.LGoFunc(excelNew))
        mod.RawSetString("open", lua.LGoFunc(excelOpen))
        mod.Immutable = true

        workbookMetatable = value.RegisterTypeMethods(nil, workbookTypeName,
            map[string]lua.LGoFunc{"__tostring": workbookToString},
            workbookMethods)

        return mod, nil
    },
}
```
