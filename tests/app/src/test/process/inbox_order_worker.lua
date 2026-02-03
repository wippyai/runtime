-- Worker: Receive multiple inbox messages and verify order
local time = require("time")

local function main()
	local inbox_ch = process.inbox()
	local received = {}

	-- Receive 5 messages
	for i = 1, 5 do
		local timeout = time.after("2s")
		local result = channel.select {
			inbox_ch:case_receive(),
			timeout:case_receive(),
		}

		if result.channel ~= inbox_ch then
			error("timeout at message " .. i)
		end

		local msg = result.value
		local payload = msg:payload():data()
		table.insert(received, payload)
	end

	-- Verify order: should be 1, 2, 3, 4, 5
	for i = 1, 5 do
		if received[i] ~= i then
			error("order_wrong: expected " .. i .. " at position " .. i .. ", got " .. tostring(received[i]))
		end
	end

	return true
end

return { main = main }
