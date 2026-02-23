-- SPDX-License-Identifier: MPL-2.0

-- Test: Fan-in pattern with multiple producers
local assert = require("assert2")

local function main()
-- Test: Multiple producers, single consumer
	local output = channel.new(10)
	local producer_count = 4
	local items_per_producer = 5

	for p = 1, producer_count do
		coroutine.spawn(function()
			for i = 1, items_per_producer do
				output:send({producer = p, item = i})
			end
		end)
	end

	local received = {}
	for _ = 1, producer_count * items_per_producer do
		local msg = output:receive()
		table.insert(received, msg)
	end

	assert.eq(#received, 20, "received all items")

	local counts = {}
	for _, msg in ipairs(received) do
		counts[msg.producer] = (counts[msg.producer] or 0) + 1
	end

	for p = 1, producer_count do
		assert.eq(counts[p], items_per_producer, "producer " .. p .. " sent all items")
	end

	return true
end

return { main = main }
