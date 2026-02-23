-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local time = require("time")

	-- Basic timer creation and wait
	local start = time.now()
	local timer = time.timer("10ms")
	assert.not_nil(timer, "timer created")

	-- Wait via channel receive
	local ch = timer:channel()
	assert.not_nil(ch, "timer channel")
	ch:receive()
	local elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 8, "timer fired")

	-- Timer with number duration
	start = time.now()
	timer = time.timer(5 * time.MILLISECOND)
	timer:channel():receive()
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 3, "timer number duration")

	-- Timer stop before firing
	timer = time.timer("100ms")
	local stopped = timer:stop()
	assert.ok(stopped, "timer stopped")

	-- Timer reset
	start = time.now()
	timer = time.timer("100ms")
	timer:reset("5ms")
	timer:channel():receive()
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() < 50, "timer reset to shorter")

	return true
end

return { main = main }
