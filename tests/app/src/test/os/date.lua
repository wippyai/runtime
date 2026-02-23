-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
-- os.date() default format
	local d = os.date()
	assert.is_string(d, "os.date() should return a string")
	assert.ok(#d > 0, "os.date() should not be empty")

	-- os.date with format specifiers
	local timestamp = os.time({year = 2024, month = 6, day = 15, hour = 14, min = 30, sec = 45})

	assert.eq(os.date("%Y", timestamp), "2024", "year format")
	assert.eq(os.date("%m", timestamp), "06", "month format")
	assert.eq(os.date("%d", timestamp), "15", "day format")
	assert.eq(os.date("%H", timestamp), "14", "hour format")
	assert.eq(os.date("%M", timestamp), "30", "minute format")
	assert.eq(os.date("%S", timestamp), "45", "second format")

	-- os.date("*t") returns table
	local tbl = os.date("*t", timestamp)
	assert.is_table(tbl, "os.date('*t') should return table")
	if type(tbl) == "table" then
		assert.eq(tbl.year, 2024, "table year")
		assert.eq(tbl.month, 6, "table month")
		assert.eq(tbl.day, 15, "table day")
		assert.eq(tbl.hour, 14, "table hour")
		assert.eq(tbl.min, 30, "table min")
		assert.eq(tbl.sec, 45, "table sec")
	end

	-- UTC format with ! prefix
	local utc_tbl = os.date("!*t", timestamp)
	assert.is_table(utc_tbl, "os.date('!*t') should return table")

	return {success = true}
end

return {main = main}
