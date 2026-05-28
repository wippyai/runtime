-- SPDX-License-Identifier: MPL-2.0

-- Worker that spawns a child, cancels it, and verifies it received CANCEL
local function main()
	local events_ch = process.events()

	-- Spawn monitored child that waits for cancel
	local child_pid, err = process.spawn_monitored("app.test.process:cancel_verify_worker", "app:processes")
	if err then
		return false, "spawn failed: " .. tostring(err)
	end

	-- Send cancel to child
	local _, cancel_err = process.cancel(child_pid)
	if cancel_err then
		return false, "cancel failed: " .. tostring(cancel_err)
	end

	-- Wait for EXIT event from child (blocking)
	local event = events_ch:receive()

	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT, got: " .. tostring(event.kind)
	end

	if event.from ~= child_pid then
		return false, "expected from child " .. child_pid .. ", got: " .. tostring(event.from)
	end

	return true
end

return { main = main }
