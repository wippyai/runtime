-- SPDX-License-Identifier: MPL-2.0

-- Worker that links to a failing process WITH trap_links=true
-- Per spec, this worker should RECEIVE LINK_DOWN event (not fail)

local time = require("time")

local function main()
-- Enable trap_links to receive LINK_DOWN events
	local ok, err = process.set_options({ trap_links = true })
	if not ok then
		return false, "set_options failed: " .. tostring(err)
	end

	-- Verify trap_links is now true
	local opts = process.get_options()
	if not opts.trap_links then
		return false, "trap_links should be true after set_options"
	end

	local events_ch = process.events()

	-- Spawn a process that will fail
	local _, err2 = process.spawn_linked(
		"app.test.process:error_exit_worker",
		"app:processes"
	)
	if err2 then
		return false, "spawn error worker failed: " .. tostring(err2)
	end

	-- Wait for LINK_DOWN event
	-- With trap_links=true, we SHOULD receive this event
	local timeout = time.after("2s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for LINK_DOWN"
	end

	local event = result.value
	if event.kind == process.event.LINK_DOWN then
		return "LINK_DOWN_RECEIVED"  -- Correct behavior
	end

	return false, "expected LINK_DOWN, got: " .. tostring(event.kind)
end

return { main = main }
