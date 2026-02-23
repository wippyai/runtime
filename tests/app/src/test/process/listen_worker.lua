-- SPDX-License-Identifier: MPL-2.0

-- Worker: Listen for messages on custom topic
local function main()
-- Subscribe to "messages" topic
	local ch = process.listen("messages")

	-- Wait for one message
	local msg = ch:receive()
	if msg then
	-- Got the message, exit
		return true
	end

	return false
end

return { main = main }
