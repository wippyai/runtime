-- SPDX-License-Identifier: MPL-2.0

-- Test: Producer-consumer pattern
local assert = require("assert2")

local function main()
-- Test basic producer-consumer with buffered channel
	local ch = channel.new(5)
	local done = channel.new(1)
	local consumed = 0

	-- Consumer
	coroutine.spawn(function()
		while true do
			local v, ok = ch:receive()
			if not ok then
				break
			end
			consumed = consumed + 1
		end
		done:send(consumed)
	end)

	-- Producer
	for i = 1, 10 do
		ch:send(i)
	end
	ch:close()

	local total = done:receive()
	assert.eq(total, 10, "all items consumed")

	-- Test ping-pong pattern
	local ping = channel.new(0)
	local pong = channel.new(0)
	local rounds_done = channel.new(1)

	coroutine.spawn(function()
		for i = 1, 5 do
			ping:receive()
			pong:send("pong")
		end
		rounds_done:send(true)
	end)

	for _ = 1, 5 do
		ping:send("ping")
		pong:receive()
	end

	local completed = rounds_done:receive()
	assert.eq(completed, true, "ping-pong completed")

	-- Test fan-out: one producer, multiple consumers
	local work = channel.new(10)
	local results = channel.new(10)

	-- 3 workers
	for _ = 1, 3 do
		coroutine.spawn(function()
			while true do
				local job, ok = work:receive()
				if not ok then
					break
				end
				results:send(job * 2)
			end
		end)
	end

	-- Send work
	for i = 1, 6 do
		work:send(i)
	end
	work:close()

	-- Collect results
	local sum = 0
	for _ = 1, 6 do
		local r = results:receive()
		sum = sum + r
	end

	-- sum of (1+2+3+4+5+6)*2 = 42
	assert.eq(sum, 42, "all work processed")

	return true
end

return { main = main }
