-- Test: excel.new function
local assert = require("assert_primitives")

local function main()
	local excel = require("excel")

	-- Create new workbook
	local wb, err = excel.new()
	assert.not_nil(wb, "workbook should not be nil")
	assert.is_nil(err, "error should be nil")

	-- Check default sheet exists
	local sheets = wb:get_sheet_list()
	assert.not_nil(sheets, "sheets should not be nil")
	assert.eq(#sheets >= 1, true, "should have at least one default sheet")

	-- Clean up
	wb:close()

	return true
end

return { main = main }
