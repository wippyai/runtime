-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local time = require("time")

	-- Basic ticker
	local start = time.now()
	local ticker = time.ticker("5ms")
	assert.not_nil(ticker, "ticker created")

	-- Receive 3 ticks
	local ticks = 0
	for _ = 1, 3 do
		ticker:channel():receive()
		ticks = ticks + 1
	end
	ticker:stop()
	local elapsed = time.now():sub(start)
	assert.eq(ticks, 3, "received 3 ticks")
	assert.ok(elapsed:milliseconds() >= 10, "ticker timing")

	-- Ticker with number duration
	start = time.now()
	ticker = time.ticker(5 * time.MILLISECOND)
	ticker:channel():receive()
	ticker:channel():receive()
	ticker:stop()
	elapsed = time.now():sub(start)
	assert.ok(elapsed:milliseconds() >= 8, "ticker number duration")

	-- Ticker stop immediately
	ticker = time.ticker("5ms")
	ticker:stop()

	-- Multiple tickers
	local ticker1 = time.ticker("3ms")
	local ticker2 = time.ticker("6ms")
	local count1, count2 = 0, 0

	start = time.now()
	while count1 < 2 or count2 < 1 do
		if count1 < 2 then
			ticker1:channel():receive()
			count1 = count1 + 1
		end
		if count2 < 1 then
			ticker2:channel():receive()
			count2 = count2 + 1
		end
	end

	ticker1:stop()
	ticker2:stop()

	assert.eq(count1, 2, "ticker1 count")
	assert.eq(count2, 1, "ticker2 count")

	return true
end

return { main = main }
