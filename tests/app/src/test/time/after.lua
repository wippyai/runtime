local assert = require("assert_primitives")

local function main()
	local time = require("time")

	-- Basic after returns a channel
	local start = time.now()
	local ch = time.after("10ms")
	assert.not_nil(ch, "after returns channel")

	-- Receive from channel waits for timer and returns time.Time
	local received_time = ch:receive()
	local elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 8, "after waits for duration")
	assert.not_nil(received_time, "receive returns time value")
	-- Verify it's a time.Time by calling a method on it
	assert.not_nil(received_time:unix(), "received value has unix() method (is time.Time)")

	-- After with number duration
	start = time.now()
	ch = time.after(5 * time.MILLISECOND)
	ch:receive()
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 3, "after number duration")

	-- After with duration object
	local d = time.parse_duration("5ms")
	start = time.now()
	ch = time.after(d)
	ch:receive()
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 3, "after duration object")

	-- Multiple afters in sequence
	start = time.now()
	time.after("3ms"):receive()
	time.after("3ms"):receive()
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 4, "multiple afters")

	-- Test channel.select with time.after
	local fast = time.after("5ms")
	local slow = time.after("50ms")

	-- Select should return when fast timer fires
	start = time.now()
	local result = channel.select{
		fast:case_receive(),
		slow:case_receive()
	}
	elapsed = time.now():sub(start)

	assert.not_nil(result, "select returned result")
	assert.eq(result.ok, true, "select ok is true")
	assert.not_nil(result.channel, "select returned channel")
	assert.eq(result.channel, fast, "fast channel won select")
	assert.ok(elapsed:milliseconds() < 40, "select returned before slow timer")

	return true
end

return { main = main }
