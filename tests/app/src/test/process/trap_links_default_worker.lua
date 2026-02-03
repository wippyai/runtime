-- Worker that links to a failing process WITHOUT trap_links
-- Per spec, this worker should FAIL (not receive LINK_DOWN event)

local time = require("time")

local function main()
	local events_ch = process.events()

	-- Verify trap_links is false (default)
	local opts = process.get_options()
	if opts.trap_links then
		return false, "trap_links should be false by default"
	end

	-- Spawn a process that will fail
	local _, err = process.spawn_linked(
		"app.test.process:error_exit_worker",
		"app:processes"
	)
	if err then
		return false, "spawn error worker failed: " .. tostring(err)
	end

	-- Wait for LINK_DOWN event
	-- With trap_links=false, we should NEVER reach the point of receiving this
	-- The process should terminate before events_ch:receive() returns
	local timeout = time.after("2s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout - but should have terminated before timeout"
	end

	-- If we get here, trap_links behavior is broken
	-- We should have terminated, not received the event
	local event = result.value
	if event.kind == process.event.LINK_DOWN then
		return "LINK_DOWN_RECEIVED"  -- This is WRONG behavior for trap_links=false
	end

	return false, "unexpected event: " .. tostring(event.kind)
end

return { main = main }
