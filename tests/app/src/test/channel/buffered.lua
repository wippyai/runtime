-- Test: Buffered channel operations
local assert = require("assert2")

local function main()
-- Test buffered channel capacity
	local ch = channel.new(3)

	-- Fill buffer (non-blocking)
	ch:send(1)
	ch:send(2)
	ch:send(3)

	-- Receive all
	local v1, ok1 = ch:receive()
	local v2, ok2 = ch:receive()
	local v3, ok3 = ch:receive()

	assert.eq(v1, 1, "first value")
	assert.eq(v2, 2, "second value")
	assert.eq(v3, 3, "third value")
	assert.eq(ok1, true, "first ok")
	assert.eq(ok2, true, "second ok")
	assert.eq(ok3, true, "third ok")

	-- Test unbuffered channel with spawn (sender blocks until receiver)
	local ch2 = channel.new(0)
	local done = channel.new(1)

	coroutine.spawn(function()
		ch2:send("from spawn")
		done:send(true)
	end)

	local val = ch2:receive()
	assert.eq(val, "from spawn", "received from spawn")

	local completed = done:receive()
	assert.eq(completed, true, "spawn completed")

	return true
end

return { main = main }
