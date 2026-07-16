-- SPDX-License-Identifier: MPL-2.0

-- Test: excel workbook rows streaming cursor
local assert = require("assert_primitives")

local function main()
	local excel = require("excel")

	local wb = excel.new()
	wb:new_sheet("Data")

	for i = 1, 12 do
		wb:set_cell_value("Data", "A" .. i, "name" .. i)
		wb:set_cell_value("Data", "B" .. i, i * 10)
	end

	-- Full iteration matches get_rows
	local expected, gerr = wb:get_rows("Data")
	assert.is_nil(gerr, "get_rows should not error")
	assert.eq(#expected, 12, "should have 12 rows")

	local cur, err = wb:rows("Data")
	assert.is_nil(err, "rows should not error")
	assert.not_nil(cur, "cursor should not be nil")

	local count = 0
	while true do
		local batch, rerr = cur:read(5)
		assert.is_nil(rerr, "read should not error")
		if batch == nil then break end
		for _, row in ipairs(batch) do
			count = count + 1
			assert.eq(row[1], expected[count][1], "cell A" .. count)
			assert.eq(row[2], expected[count][2], "cell B" .. count)
		end
	end
	assert.eq(count, 12, "cursor should yield all rows")

	-- EOF is stable
	local tail, terr = cur:read()
	assert.is_nil(tail, "EOF should return nil rows")
	assert.is_nil(terr, "EOF should not error")

	local cerr = cur:close()
	assert.is_nil(cerr, "close should not error")

	-- Early close
	local cur2, err2 = wb:rows("Data")
	assert.is_nil(err2, "rows should not error")

	local batch2, rerr2 = cur2:read(3)
	assert.is_nil(rerr2, "read should not error")
	assert.eq(#batch2, 3, "batch should hold 3 rows")

	local cerr2 = cur2:close()
	assert.is_nil(cerr2, "early close should not error")

	local after, aerr = cur2:read()
	assert.is_nil(after, "read after close should return nil rows")
	assert.not_nil(aerr, "read after close should error")

	-- Idempotent close
	assert.is_nil(cur2:close(), "second close should not error")

	wb:close()
	return true
end

return { main = main }
