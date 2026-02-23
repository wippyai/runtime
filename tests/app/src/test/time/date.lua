-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local time = require("time")

	local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)
	assert.not_nil(t, "date() returns value")

	assert.eq(t:year(), 2024, "year matches")
	assert.eq(t:month(), 12, "month matches")
	assert.eq(t:day(), 29, "day matches")
	assert.eq(t:hour(), 15, "hour matches")
	assert.eq(t:minute(), 4, "minute matches")
	assert.eq(t:second(), 5, "second matches")
	assert.eq(t:nanosecond(), 0, "nanosecond matches")

	local unix_t = time.unix(1735484645, 0)
	local utc = unix_t:utc()
	assert.eq(utc:year(), 2024, "unix year matches")
	assert.eq(utc:month(), 12, "unix month matches")
	assert.eq(utc:day(), 29, "unix day matches")
	assert.eq(utc:hour(), 15, "unix hour matches")

	return true
end

return { main = main }
