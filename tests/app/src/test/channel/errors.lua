-- SPDX-License-Identifier: MPL-2.0

-- Test: Channel error handling
local assert = require("assert2")

local function main()
-- Test 1: Receive on closed empty channel returns nil, false
	local ch1 = channel.new(0)
	ch1:close()

	local v, ok = ch1:receive()
	assert.eq(v, nil, "receive on closed empty returns nil")
	assert.eq(ok, false, "receive on closed empty returns ok=false")

	-- Test 2: Multiple receives on closed channel
	for _ = 1, 3 do
		local v2, ok2 = ch1:receive()
		assert.eq(v2, nil, "repeated receive returns nil")
		assert.eq(ok2, false, "repeated receive returns ok=false")
	end

	-- Test 3: Receive drains buffered values after close
	local ch9 = channel.new(3)
	ch9:send(1)
	ch9:send(2)
	ch9:send(3)
	ch9:close()

	local vals = {}
	while true do
		local v, ok = ch9:receive()
		if not ok then
			break
		end
		table.insert(vals, v)
	end

	assert.eq(#vals, 3, "received all buffered values after close")
	assert.eq(vals[1], 1, "first value correct")
	assert.eq(vals[2], 2, "second value correct")
	assert.eq(vals[3], 3, "third value correct")

	-- Test 4: Select case_send on full buffered channel with default
	local ch10 = channel.new(1)
	ch10:send("full")

	local result3 = channel.select{
		ch10:case_send("blocked"),
		default = true
	}

	assert.eq(result3.default, true, "select used default on full channel")

	return true
end

return { main = main }
