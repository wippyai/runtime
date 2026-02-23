<!-- SPDX-License-Identifier: MPL-2.0 -->

# excel

Excel file operations for reading and writing .xlsx workbooks. IO, encoding, deterministic.

## Loading

```lua
local excel = require("excel")
```

## Functions

### new() → Workbook, error

Creates a new empty Excel workbook.

**Returns:**
- Success: `Workbook, nil` - new workbook with default "Sheet1"
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |

### open(reader: File) → Workbook, error

Opens an Excel workbook from a reader object.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| reader | File | yes | - | Must implement io.Reader (e.g., fs.File) |

**Returns:**
- Success: `Workbook, nil` - opened workbook
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INTERNAL | no |
| not a reader | argument error (Lua runtime) | - |
| invalid Excel file | errors.INTERNAL | no |
| empty file | errors.INTERNAL | no |

## Types

### Workbook

Returned by `excel.new()` and `excel.open()`. Represents an Excel .xlsx workbook.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| new_sheet | (name: string) → integer, error | Sheet index | Creates sheet or returns existing index |
| get_sheet_list | () → string[], error | Sheet names | Returns all sheet names as array |
| get_rows | (sheet: string) → string[][], error | 2D array of cells | All values as strings |
| set_cell_value | (sheet: string, cell: string, value: any) → error | - | Sets single cell value |
| write_to | (writer: File) → error | - | Writes workbook to writer |
| close | () → error | - | Closes workbook, releases resources |

#### workbook:new_sheet(name: string) → integer, error

Creates a new sheet or returns existing sheet index.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Sheet name |

**Returns:**
- Success: `integer, nil` - sheet index (1-based)
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid workbook | errors.INVALID | no |
| workbook closed | errors.INTERNAL | no |

**Notes:**
- If sheet with same name exists, returns existing sheet index
- No error on duplicate name

#### workbook:get_sheet_list() → string[], error

Returns list of all sheet names in workbook.

**Returns:**
- Success: `string[], nil` - array of sheet names
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid workbook | errors.INVALID | no |
| workbook closed | errors.INTERNAL | no |

**Notes:**
- New workbooks have at least one default sheet (typically "Sheet1")
- Order matches sheet order in workbook

#### workbook:get_rows(sheet: string) → string[][], error

Gets all rows from a sheet as 2D array.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sheet | string | yes | - | Sheet name |

**Returns:**
- Success: `string[][], nil` - 2D array where rows[row][col] is cell value
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid workbook | errors.INVALID | no |
| workbook closed | errors.INTERNAL | no |
| non-existent sheet | errors.INTERNAL | no |

**Notes:**
- All cell values returned as strings (numbers, booleans converted)
- Empty sheet returns empty table `{}`
- Booleans returned as "TRUE" or "FALSE" (uppercase)
- Numbers returned as string representation ("123", "45.67")
- Arrays are 1-indexed (Lua convention)

#### workbook:set_cell_value(sheet: string, cell: string, value: any) → error

Sets value of a single cell.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sheet | string | yes | - | Sheet name |
| cell | string | yes | - | Cell reference ("A1", "B2", "AA100") |
| value | any | yes | - | string, integer, number, boolean |

**Returns:**
- Success: `nil`
- Error: `error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid workbook | errors.INTERNAL | no |
| workbook closed | errors.INTERNAL | no |
| non-existent sheet | errors.INTERNAL | no |
| invalid cell reference | errors.INTERNAL | no |

**Notes:**
- Cell reference format: column letter(s) + row number
- Column letters are case-insensitive but uppercase preferred
- Supports types: string, integer (Lua 5.3), number (float), boolean
- Create sheet first with `new_sheet()` before setting cells

#### workbook:write_to(writer: File) → error

Writes workbook to a writer object.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| writer | File | yes | - | Must implement io.Writer (e.g., fs.File) |

**Returns:**
- Success: `nil`
- Error: `error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid workbook | errors.INTERNAL | no |
| workbook closed | errors.INTERNAL | no |
| not a writer | errors.INTERNAL | no |
| write failed | errors.INTERNAL | no |

**Notes:**
- Writer must be opened for writing before calling
- Does not close the writer
- Call `writer:close()` separately after writing

#### workbook:close() → error

Closes workbook and releases resources.

**Returns:**
- Success: `nil`
- Error: `error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid workbook | errors.INTERNAL | no |

**Notes:**
- Idempotent - safe to call multiple times
- All operations after close will error with "workbook is closed"
- Workbooks are automatically closed when Lua context ends
- Best practice: always call close when done

## Dependencies

### File (from fs module)

File objects implement Go's io.Reader and io.Writer interfaces, used by `excel.open()` and `workbook:write_to()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| close | () → error | - | Close file handle |

See: `runtime/lua/modules/fs/spec.md`

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local wb, err = excel.open(file)
if err then
    if err:kind() == errors.INVALID then
        -- invalid workbook userdata
    elseif err:kind() == errors.INTERNAL then
        -- operation failed (non-existent sheet, closed workbook, etc.)
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

All errors are non-retryable (`err:retryable() == false`).

## Example

```lua
local excel = require("excel")
local fs = require("fs")

-- Create new workbook
local wb = excel.new()

-- Create sheet
wb:new_sheet("Sales")

-- Set headers
wb:set_cell_value("Sales", "A1", "Month")
wb:set_cell_value("Sales", "B1", "Revenue")
wb:set_cell_value("Sales", "C1", "Expenses")

-- Set data
wb:set_cell_value("Sales", "A2", "Jan")
wb:set_cell_value("Sales", "B2", 10000)
wb:set_cell_value("Sales", "C2", 8000)

wb:set_cell_value("Sales", "A3", "Feb")
wb:set_cell_value("Sales", "B3", 12000)
wb:set_cell_value("Sales", "C3", 8500)

-- Read rows back
local rows, err = wb:get_rows("Sales")
if err then error(err) end

for i, row in ipairs(rows) do
    print("Row " .. i .. ": " .. row[1] .. ", " .. row[2] .. ", " .. row[3])
end
-- Output:
-- Row 1: Month, Revenue, Expenses
-- Row 2: Jan, 10000, 8000
-- Row 3: Feb, 12000, 8500

-- Write to file
local file, err = fs.create("report.xlsx")
if err then error(err) end

local err = wb:write_to(file)
if err then error(err) end

file:close()
wb:close()

-- Open existing file
local file2, err = fs.open("report.xlsx")
if err then error(err) end

local wb2, err = excel.open(file2)
if err then error(err) end

local rows2 = wb2:get_rows("Sales")
-- Access data: rows2[2][1] == "Jan"

wb2:close()
file2:close()
```
