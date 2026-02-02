-- Worker that explicitly links to a target PID
-- Receives target PID from sender, links to it, confirms link, waits for LINK_DOWN
local time = require("time")

local function main()
-- Enable trap_links to receive LINK_DOWN events
	local ok, err = process.set_options({ trap_links = true })
	if not ok then
		return false, "set_options failed: " .. tostring(err)
	end

	local events_ch = process.events()
	local inbox_ch = process.inbox()

	-- Wait for target PID from main
	local msg = inbox_ch:receive()
	if not msg then
		return false, "nil message"
	end

	local sender = msg:from()
	if not sender then
		return false, "no sender"
	end

	local payload = msg:payload()
	if not payload then
		return false, "no payload"
	end

	local target_pid = string(payload:data())
	if not target_pid then
		return false, "no target pid in payload"
	end

	-- Explicitly link to target
	local ok, err = process.link(target_pid)
	if not ok then
		return false, "link failed: " .. tostring(err)
	end

	-- Notify main (sender) that we're linked
	process.send(sender, "linked", process.pid())

	-- Wait for LINK_DOWN from target
	local timeout = time.after("3s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for LINK_DOWN"
	end

	local event = result.value
	if event.kind == process.event.LINK_DOWN then
		return "LINK_DOWN_RECEIVED"
	end

	return false, "expected LINK_DOWN, got kind=" .. tostring(event.kind)
end

return { main = main }
