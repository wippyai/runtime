-- SPDX-License-Identifier: MPL-2.0

-- Test: Channel close behavior
local assert = require("assert2")

local function main()
-- Test close on empty channel
	local ch = channel.new(0)
	ch:close()

	-- Receive from closed empty channel returns (nil, false)
	local v, ok = ch:receive()
	assert.is_nil(v, "nil from closed channel")
	assert.eq(ok, false, "ok is false from closed channel")

	-- Test close on buffered channel with value
	local ch2 = channel.new(1)
	ch2:send("buffered")
	ch2:close()

	-- First receive gets buffered value
	local v1, ok1 = ch2:receive()
	assert.eq(v1, "buffered", "got buffered value")
	assert.eq(ok1, true, "ok is true for buffered")

	-- Second receive gets closed signal
	local v2, ok2 = ch2:receive()
	assert.is_nil(v2, "nil after drain")
	assert.eq(ok2, false, "ok is false after drain")

	-- Test close wakes blocked receiver via spawn
	local ch3 = channel.new(0)
	local result = channel.new(1)

	coroutine.spawn(function()
		local v, ok = ch3:receive()
		result:send({value = v, ok = ok})
	end)

	ch3:close()

	local r = result:receive()
	assert.is_nil(r.value, "receiver got nil")
	assert.eq(r.ok, false, "receiver got ok=false")

	return true
end

return { main = main }
