-- Worker: Use inbox, events, and listen together
local time = require("time")

local function main()
	local inbox_ch = process.inbox()
	local events_ch = process.events()
	local custom_ch = process.listen("custom")

	local received = {
		inbox = false,
		custom = false,
		cancel = false,
	}

	-- Receive from all three channels until we get cancel
	while not received.cancel do
		local timeout = time.after("5s")
		local result = channel.select {
			inbox_ch:case_receive(),
			events_ch:case_receive(),
			custom_ch:case_receive(),
			timeout:case_receive(),
		}

		if result.channel == timeout then
			error("timeout waiting for messages/cancel")
		end

		if result.channel == inbox_ch then
			received.inbox = true
		elseif result.channel == custom_ch then
			received.custom = true
		elseif result.channel == events_ch then
			local event = result.value
			if event.kind == process.event.CANCEL then
				received.cancel = true
			end
		end
	end

	return received
end

return { main = main }
