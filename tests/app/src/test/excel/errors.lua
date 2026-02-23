-- SPDX-License-Identifier: MPL-2.0

-- Test: excel error handling
local assert = require("assert_primitives")

local function main()
	local excel = require("excel")

	local wb = excel.new()

	-- Non-existent sheet error
	local rows, err = wb:get_rows("NonExistent")
	assert.is_nil(rows, "rows should be nil for non-existent sheet")
	assert.not_nil(err, "error should not be nil")
	assert.eq(err:kind(), errors.INTERNAL, "error kind should be INTERNAL")
	assert.eq(err:retryable(), false, "error should not be retryable")

	-- Set cell on non-existent sheet
	local set_err = wb:set_cell_value("NonExistent", "A1", "test")
	assert.not_nil(set_err, "set_cell_value error should not be nil")
	assert.eq(set_err:kind(), errors.INTERNAL, "set error kind should be INTERNAL")

	-- Close workbook
	wb:close()

	-- Operations on closed workbook
	local sheets, closed_err = wb:get_sheet_list()
	assert.is_nil(sheets, "sheets should be nil for closed workbook")
	assert.not_nil(closed_err, "error should not be nil for closed workbook")
	assert.eq(closed_err:kind(), errors.INTERNAL, "closed error kind should be INTERNAL")

	return true
end

return { main = main }
