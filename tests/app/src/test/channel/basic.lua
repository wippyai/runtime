-- SPDX-License-Identifier: MPL-2.0

-- Test: Basic channel operations
local assert = require("assert2")

local function main()
-- Test channel.new exists
	assert.not_nil(channel, "channel module exists")
	assert.not_nil(channel.new, "channel.new exists")

	-- Test buffered channel send/receive (non-blocking when buffer available)
	local ch = channel.new(1)
	ch:send("hello")
	local val, ok = ch:receive()
	assert.eq(val, "hello", "received correct value")
	assert.eq(ok, true, "receive ok is true")

	-- Test channel methods
	local ch2 = channel.new(0)
	assert.not_nil(ch2.send, "channel has send method")
	assert.not_nil(ch2.receive, "channel has receive method")
	assert.not_nil(ch2.close, "channel has close method")
	assert.not_nil(ch2.case_send, "channel has case_send method")
	assert.not_nil(ch2.case_receive, "channel has case_receive method")

	-- Test multiple values through buffered channel
	local ch3 = channel.new(3)
	ch3:send(1)
	ch3:send(2)
	ch3:send(3)

	local v1 = ch3:receive()
	local v2 = ch3:receive()
	local v3 = ch3:receive()

	assert.eq(v1, 1, "first value")
	assert.eq(v2, 2, "second value")
	assert.eq(v3, 3, "third value")

	return true
end

return { main = main }
