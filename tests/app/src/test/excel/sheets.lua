-- Test: excel workbook sheet operations
local assert = require("assert_primitives")

local function main()
	local excel = require("excel")

	local wb = excel.new()

	-- Create new sheet
	local index, err = wb:new_sheet("TestSheet")
	assert.not_nil(index, "sheet index should not be nil")
	assert.is_nil(err, "error should be nil")

	-- Create more sheets
	wb:new_sheet("Sheet2")
	wb:new_sheet("Sheet3")

	-- Get sheet list
	local sheets = wb:get_sheet_list()
	assert.not_nil(sheets, "sheets should not be nil")

	-- Verify sheets exist
	local found = {}
	for _, name in ipairs(sheets) do
		found[name] = true
	end
	assert.eq(found["TestSheet"], true, "TestSheet should exist")
	assert.eq(found["Sheet2"], true, "Sheet2 should exist")
	assert.eq(found["Sheet3"], true, "Sheet3 should exist")

	-- Duplicate sheet name returns same index
	local index2 = wb:new_sheet("TestSheet")
	assert.eq(index, index2, "duplicate sheet name returns same index")

	wb:close()
	return true
end

return { main = main }
