-- SPDX-License-Identifier: MPL-2.0

-- Test: excel workbook bytes serialization
local assert = require("assert_primitives")

local function main()
	local excel = require("excel")

	local wb = excel.new()
	wb:new_sheet("Data")
	wb:set_cell_value("Data", "A1", "Name")
	wb:set_cell_value("Data", "B1", "Score")
	wb:set_cell_value("Data", "A2", "Alice")
	wb:set_cell_value("Data", "B2", 42)

	-- Serialize to a binary string
	local data, err = wb:bytes()
	assert.is_nil(err, "bytes should succeed")
	assert.is_string(data, "data should be a string")
	assert.eq(data:sub(1, 2), "PK", "data should be a zip archive (xlsx)")
	assert.ok(#data > 0, "data should not be empty")

	-- Workbook remains usable after bytes()
	local rows, rows_err = wb:get_rows("Data")
	assert.is_nil(rows_err, "workbook should remain usable after bytes")
	assert.eq(rows[2][1], "Alice", "cell value should be intact")

	-- bytes() on a closed workbook errors
	wb:close()
	local data2, err2 = wb:bytes()
	assert.is_nil(data2, "data should be nil on closed workbook")
	assert.not_nil(err2, "closed workbook should error")

	return true
end

return { main = main }
