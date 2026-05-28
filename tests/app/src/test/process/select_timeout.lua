-- SPDX-License-Identifier: MPL-2.0

-- Test: channel.select with process.events() and time.after
-- This ensures channel.select works correctly with external event channels
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn a worker
	local worker_pid, err = process.spawn_monitored("app.test.process:long_worker", "app:processes")
	if err then
		return false, "spawn failed: " .. tostring(err)
	end

	-- Give worker time to call process.events()
	time.sleep("10ms")

	-- Cancel worker
	process.cancel(worker_pid)

	-- Use channel.select with both events channel AND a timeout
	-- The events channel should fire BEFORE the 5 second timeout
	local timeout_ch = time.after("5s")

	local result = channel.select{
		events_ch:case_receive(),
		timeout_ch:case_receive()
	}

	-- Should have selected events_ch, not timeout
	if result.channel ~= events_ch then
		return false, "expected events channel, got timeout"
	end

	local event = result.value
	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT, got: " .. tostring(event.kind)
	end

	return true
end

return { main = main }
