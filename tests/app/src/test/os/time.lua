local assert = require("assert_primitives")

local function main()
-- os.time() without args returns current timestamp
	local now = os.time()
	assert.is_number(now, "os.time() should return a number")
	assert.ok(now > 0, "os.time() should return positive timestamp")

	-- os.time() with table
	local t = os.time({year = 2024, month = 6, day = 15, hour = 12, min = 30, sec = 45})
	assert.is_number(t, "os.time(table) should return a number")

	-- os.time() with partial table (defaults to 0 for missing time fields)
	local t2 = os.time({year = 2024, month = 1, day = 1})
	assert.is_number(t2, "os.time(partial) should return a number")

	return {success = true}
end

return {main = main}
