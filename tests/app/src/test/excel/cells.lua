-- SPDX-License-Identifier: MPL-2.0

-- Test: excel workbook cell operations
local assert = require("assert_primitives")

local function main()
	local excel = require("excel")

	local wb = excel.new()
	wb:new_sheet("Data")

	-- Set string value
	local err = wb:set_cell_value("Data", "A1", "Hello")
	assert.is_nil(err, "set string should not error")

	-- Set number value
	err = wb:set_cell_value("Data", "B1", 123)
	assert.is_nil(err, "set number should not error")

	-- Set float value
	err = wb:set_cell_value("Data", "C1", 45.67)
	assert.is_nil(err, "set float should not error")

	-- Set boolean value
	err = wb:set_cell_value("Data", "D1", true)
	assert.is_nil(err, "set boolean should not error")

	err = wb:set_cell_value("Data", "E1", false)
	assert.is_nil(err, "set boolean false should not error")

	-- Read rows back
	local rows = wb:get_rows("Data")
	assert.not_nil(rows, "rows should not be nil")
	assert.eq(#rows >= 1, true, "should have at least one row")

	-- Verify values (all returned as strings)
	assert.eq(rows[1][1], "Hello", "string value preserved")
	assert.eq(rows[1][2], "123", "number as string")
	assert.eq(rows[1][3], "45.67", "float as string")
	assert.eq(rows[1][4], "TRUE", "boolean true as uppercase string")
	assert.eq(rows[1][5], "FALSE", "boolean false as uppercase string")

	wb:close()
	return true
end

return { main = main }
