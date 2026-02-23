-- SPDX-License-Identifier: MPL-2.0

-- Test: excel complete workflow
local assert = require("assert_primitives")

local function main()
	local excel = require("excel")

	-- Create workbook with sales data
	local wb = excel.new()
	wb:new_sheet("Sales")

	-- Set headers
	wb:set_cell_value("Sales", "A1", "Month")
	wb:set_cell_value("Sales", "B1", "Revenue")
	wb:set_cell_value("Sales", "C1", "Expenses")
	wb:set_cell_value("Sales", "D1", "Profit")

	-- Set data
	local months = {"Jan", "Feb", "Mar"}
	local revenues = {10000, 12000, 9500}
	local expenses = {8000, 8500, 7800}

	for i = 1, 3 do
		local row = i + 1
		wb:set_cell_value("Sales", "A" .. row, months[i])
		wb:set_cell_value("Sales", "B" .. row, revenues[i])
		wb:set_cell_value("Sales", "C" .. row, expenses[i])
		wb:set_cell_value("Sales", "D" .. row, revenues[i] - expenses[i])
	end

	-- Verify sheet exists
	local sheets = wb:get_sheet_list()
	local found = false
	for _, name in ipairs(sheets) do
		if name == "Sales" then
			found = true
		end
	end
	assert.eq(found, true, "Sales sheet should exist")

	-- Verify data
	local rows = wb:get_rows("Sales")
	assert.eq(#rows, 4, "should have 4 rows")

	-- Check headers
	assert.eq(rows[1][1], "Month", "header A1")
	assert.eq(rows[1][4], "Profit", "header D1")

	-- Check data rows
	assert.eq(rows[2][1], "Jan", "first month")
	assert.eq(rows[2][2], "10000", "Jan revenue")
	assert.eq(rows[3][3], "8500", "Feb expenses")
	assert.eq(rows[4][4], "1700", "Mar profit")

	wb:close()
	return true
end

return { main = main }
