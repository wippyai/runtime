-- Test: excel workbook get_rows
local assert = require("assert_primitives")

local function main()
    local excel = require("excel")

    local wb = excel.new()
    wb:new_sheet("Report")

    -- Set up header row
    wb:set_cell_value("Report", "A1", "Name")
    wb:set_cell_value("Report", "B1", "Age")
    wb:set_cell_value("Report", "C1", "Active")

    -- Set up data rows
    wb:set_cell_value("Report", "A2", "Alice")
    wb:set_cell_value("Report", "B2", 30)
    wb:set_cell_value("Report", "C2", true)

    wb:set_cell_value("Report", "A3", "Bob")
    wb:set_cell_value("Report", "B3", 25)
    wb:set_cell_value("Report", "C3", false)

    -- Get rows
    local rows, err = wb:get_rows("Report")
    assert.is_nil(err, "get_rows should not error")
    assert.not_nil(rows, "rows should not be nil")
    assert.eq(#rows, 3, "should have 3 rows")

    -- Check header
    assert.eq(rows[1][1], "Name", "header A1")
    assert.eq(rows[1][2], "Age", "header B1")
    assert.eq(rows[1][3], "Active", "header C1")

    -- Check data
    assert.eq(rows[2][1], "Alice", "data A2")
    assert.eq(rows[2][2], "30", "data B2")
    assert.eq(rows[2][3], "TRUE", "data C2")

    assert.eq(rows[3][1], "Bob", "data A3")
    assert.eq(rows[3][2], "25", "data B3")
    assert.eq(rows[3][3], "FALSE", "data C3")

    -- Empty sheet returns empty table
    wb:new_sheet("Empty")
    local empty_rows = wb:get_rows("Empty")
    assert.not_nil(empty_rows, "empty rows should not be nil")
    assert.eq(#empty_rows, 0, "empty sheet has 0 rows")

    wb:close()
    return true
end

return { main = main }
