-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local time = require("time")

	local t = time.now()
	assert.not_nil(t, "now() returns value")
	assert.eq(type(t), "userdata", "now() returns userdata")

	local hour = t:hour()
	assert.ok(hour >= 0 and hour <= 23, "hour in valid range")

	local minute = t:minute()
	assert.ok(minute >= 0 and minute <= 59, "minute in valid range")

	local second = t:second()
	assert.ok(second >= 0 and second <= 59, "second in valid range")

	return true
end

return { main = main }
