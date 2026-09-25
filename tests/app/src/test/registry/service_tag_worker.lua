-- SPDX-License-Identifier: MPL-2.0

-- Supervised worker that registers a name and answers pings with the tag
-- its service definition passes as input.
local function main(tag, name)
	local inbox_ch = process.inbox()
	local events_ch = process.events()

	local _, err = process.registry.register(name)
	if err then
		return false, "register failed: " .. tostring(err)
	end

	while true do
		local result = channel.select {
			inbox_ch:case_receive(),
			events_ch:case_receive(),
		}

		if result.channel == events_ch then
			if result.value.kind == process.event.CANCEL then
				return "cancelled"
			end
		else
			local msg = result.value
			if msg and msg:topic() == "ping" then
				local data = msg:payload():data()
				process.send(string(data.reply_to), "pong", { tag = tag, pid = process.pid() })
			end
		end
	end
end

return { main = main }
