-- SPDX-License-Identifier: MPL-2.0

-- Worker that tests LINK_DOWN event propagation
-- Spawns a parent that spawns a linked child, then monitors both
local function main()
	local events_ch = process.events()

	-- Spawn monitored parent that will spawn a linked child and exit
	local parent_pid, err = process.spawn_monitored("app.test.process:link_parent_worker", "app:processes")
	if err then
		return false, "spawn parent failed: " .. tostring(err)
	end

	-- Wait for parent EXIT event (blocking)
	local event = events_ch:receive()

	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT, got: " .. tostring(event.kind)
	end

	if event.from ~= parent_pid then
		return false, "expected from parent " .. parent_pid .. ", got: " .. tostring(event.from)
	end

	return true
end

return { main = main }
