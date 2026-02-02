local assert = require("assert_primitives")

local function main()
	local time = require("time")

	local d = time.parse_duration("1h30m45s")

	assert.ok(d:nanoseconds() > 0, "nanoseconds > 0")
	assert.ok(d:microseconds() > 0, "microseconds > 0")
	assert.ok(d:milliseconds() > 0, "milliseconds > 0")
	assert.ok(d:seconds() >= 5445, "seconds >= 5445")
	assert.ok(d:minutes() >= 90, "minutes >= 90")
	assert.ok(d:hours() >= 1.5, "hours >= 1.5")

	local str = tostring(d)
	assert.not_nil(str, "duration tostring works")
	assert.ok(#str > 0, "duration string not empty")

	return true
end

return { main = main }
