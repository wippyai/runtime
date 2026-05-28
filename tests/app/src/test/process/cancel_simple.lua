-- SPDX-License-Identifier: MPL-2.0

-- Simple cancel test - no timeout, direct check
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

	-- Wait for EXIT (blocking - will hang if cancel doesn't work)
	local event = events_ch:receive()

	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT, got: " .. tostring(event.kind)
	end

	return true
end

return { main = main }
