local assert = require("assert_primitives")

local function main()
-- difftime returns difference in seconds
	local t1 = os.time({year = 2024, month = 1, day = 1, hour = 0, min = 0, sec = 0})
	local t2 = os.time({year = 2024, month = 1, day = 1, hour = 0, min = 0, sec = 30})

	local diff = os.difftime(t2, t1)
	assert.eq(diff, 30, "difftime should be 30 seconds")

	-- negative difference
	local diff_neg = os.difftime(t1, t2)
	assert.eq(diff_neg, -30, "difftime should be -30 seconds")

	-- larger difference (1 hour)
	local t3 = os.time({year = 2024, month = 1, day = 1, hour = 1, min = 0, sec = 0})
	local hour_diff = os.difftime(t3, t1)
	assert.eq(hour_diff, 3600, "difftime should be 3600 seconds (1 hour)")

	return {success = true}
end

return {main = main}
