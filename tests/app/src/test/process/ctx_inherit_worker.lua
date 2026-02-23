-- SPDX-License-Identifier: MPL-2.0

-- Worker that spawns a grandchild and checks if it inherits security context
local security = require("security")
local time = require("time")

local function main()
-- First, verify WE have actor/scope (we were spawned with_context)
	local actor = security.actor()
	if not actor then
		error("parent worker: actor not found")
	end

	local scope = security.scope()
	if not scope then
		error("parent worker: scope not found")
	end

	local events_ch = process.events()

	-- Now spawn a grandchild WITHOUT with_context - just plain spawn
	-- This should inherit our actor/scope automatically
	local _, err = process.spawn_monitored(
		"app.test.process:ctx_inherit_grandchild_worker",
		"app:processes"
	)

	if err then
		error("failed to spawn grandchild: " .. tostring(err))
	end

	-- Wait for grandchild to exit
	local timeout = time.after("3s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		error("timeout waiting for grandchild")
	end

	local event = result.value
	if event.kind ~= process.event.EXIT then
		error("expected EXIT event, got " .. tostring(event.kind))
	end

	if event.error then
	-- Grandchild failed - propagate the error
		error("grandchild failed: " .. tostring(event.error))
	end

	return true
end

return { main = main }
