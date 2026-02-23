-- SPDX-License-Identifier: MPL-2.0

-- Worker that spawns a short-lived child and waits for EXIT event
local function main()
	local events_ch = process.events()

	-- Spawn monitored short-lived worker
	local child_pid, err = process.spawn_monitored("app.test.process:short_worker", "app:processes")
	if err then
		return false, "spawn failed: " .. tostring(err)
	end

	-- Wait for EXIT event (blocking - test timeout handles deadlock)
	local event = events_ch:receive()

	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT, got: " .. tostring(event.kind)
	end

	if event.from ~= child_pid then
		return false, "expected from " .. child_pid .. ", got: " .. tostring(event.from)
	end

	return true
end

return { main = main }
