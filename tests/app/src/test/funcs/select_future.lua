-- Test: channel.select with futures
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Start two async operations with different delays
	local fast = funcs.async("app.test.funcs:slow", "20ms", "fast")
	local slow = funcs.async("app.test.funcs:slow", "200ms", "slow")

	-- Get channels from futures
	local fast_ch = fast:channel()
	local slow_ch = slow:channel()

	assert.not_nil(fast_ch, "fast future has channel")
	assert.not_nil(slow_ch, "slow future has channel")

	-- Verify channel identity works
	local fast_ch2 = fast:channel()
	assert.eq(fast_ch, fast_ch2, "future:channel() returns same channel")

	-- Select on both channels using case_receive
	-- V1 API: channel.select{cases...} returns {channel, value, ok}
	local result = channel.select{
		fast_ch:case_receive(),
		slow_ch:case_receive()
	}

	-- Result should be a table with channel, value, ok
	assert.is_table(result, "select returned table")
	assert.not_nil(result.channel, "result has channel")
	assert.eq(result.ok, true, "result ok is true")

	-- The channel should be fast_ch or slow_ch
	local is_fast = result.channel == fast_ch
	local is_slow = result.channel == slow_ch
	assert.eq(is_fast or is_slow, true, "result channel is one of the expected channels")

	-- Wait for slow one too
	local slow_payload = slow:response():receive()
	assert.not_nil(slow_payload, "slow result available")
	assert.eq(slow_payload:data().value, "slow", "slow result correct")

	return true
end

return { main = main }
