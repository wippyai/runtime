-- SPDX-License-Identifier: MPL-2.0

-- Worker that waits for events (CANCEL or termination)
local function main()
	local events_ch = process.events()

	-- Wait for event (blocking)
	local event = events_ch:receive()

	if event.kind == process.event.CANCEL then
		return "cancelled"
	end

	return "event: " .. tostring(event.kind)
end

return { main = main }
