-- SPDX-License-Identifier: MPL-2.0

-- Worker that spawns a child, cancels it, and waits for EXIT
local function main()
	local events_ch = process.events()

	-- Spawn monitored long-running worker
	local child_pid, err = process.spawn_monitored("app.test.process:long_worker", "app:processes")
	if err then
		return false, "spawn failed: " .. tostring(err)
	end

	-- Cancel the child process (child should be ready immediately after spawn)
	local _, cancel_err = process.cancel(child_pid)
	if cancel_err then
		return false, "cancel failed: " .. tostring(cancel_err)
	end

	-- Wait for EXIT event (blocking)
	local event = events_ch:receive()

	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT, got: " .. tostring(event.kind)
	end

	return true
end

return { main = main }
