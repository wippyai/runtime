-- SPDX-License-Identifier: MPL-2.0

-- Worker: Listen on multiple topics and receive from both
local time = require("time")

local function main()
-- Subscribe to two different topics with message mode to get :topic() and :payload()
	local ch_a = process.listen("topic_a", { message = true })
	local ch_b = process.listen("topic_b", { message = true })

	local received = {}
	local count = 0

	-- Receive 2 messages from either topic
	while count < 2 do
		local timeout = time.after("3s")
		local result = channel.select {
			ch_a:case_receive(),
			ch_b:case_receive(),
			timeout:case_receive(),
		}

		if result.channel == timeout then
			error("timeout after " .. count .. " messages")
		end

		local msg = result.value
		local topic = msg:topic()
		local payload = msg:payload()

		received[topic] = payload
		count = count + 1
	end

	return received
end

return { main = main }
