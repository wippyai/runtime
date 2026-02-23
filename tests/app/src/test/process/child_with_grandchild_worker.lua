-- SPDX-License-Identifier: MPL-2.0

-- Child that spawns a linked grandchild, then waits for events
local time = require("time")

local function main()
-- Enable trap_links to receive LINK_DOWN events
	local ok, err = process.set_options({ trap_links = true })
	if not ok then
		return false, "set_options failed: " .. tostring(err)
	end

	local events_ch = process.events()
	if not events_ch then
		return false, "failed to get events channel"
	end

	-- Spawn linked grandchild
	local _, err = process.spawn_linked("app.test.process:grandchild_worker", "app:processes")
	if err then
		return false, "spawn grandchild failed: " .. tostring(err)
	end

	-- Wait for events (LINK_DOWN from parent, CANCEL, etc)
	local timeout = time.after("30s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == events_ch then
		local event = result.value
		if event then
			return "event:" .. tostring(event.kind)
		end
	end

	return "timeout"
end

return { main = main }
