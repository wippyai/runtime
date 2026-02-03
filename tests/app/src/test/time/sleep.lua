local assert = require("assert_primitives")

local function main()
	local time = require("time")

	-- Basic sleep with number (nanoseconds)
	local start = time.now()
	time.sleep(10 * time.MILLISECOND)
	local elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 8, "sleep 10ms")

	-- Sleep with string duration
	start = time.now()
	time.sleep("5ms")
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 3, "sleep string 5ms")

	-- Sleep with duration object
	local d = time.parse_duration("5ms")
	start = time.now()
	time.sleep(d)
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 3, "sleep duration object")

	-- Multiple sequential sleeps
	start = time.now()
	time.sleep("2ms")
	time.sleep("2ms")
	time.sleep("2ms")
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 4, "multiple sleeps")

	return true
end

return { main = main }
