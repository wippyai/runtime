-- SPDX-License-Identifier: MPL-2.0

-- Debug worker: Wait for CANCEL event
local function main()
	io.write("DEBUG WORKER: Starting, pid=" .. tostring(process.pid()) .. "\n")
	io.flush()

	local events_ch = process.events()
	io.write("DEBUG WORKER: Got events channel, waiting for event\n")
	io.flush()

	-- Wait for any event
	local event = events_ch:receive()

	io.write("DEBUG WORKER: Received event kind=" .. tostring(event.kind) .. "\n")
	io.flush()

	if event.kind == process.event.CANCEL then
		io.write("DEBUG WORKER: Got CANCEL, exiting\n")
		io.flush()
		return "cancelled"
	end

	io.write("DEBUG WORKER: Got unexpected event: " .. tostring(event.kind) .. "\n")
	io.flush()
	return "unexpected: " .. tostring(event.kind)
end

return { main = main }
