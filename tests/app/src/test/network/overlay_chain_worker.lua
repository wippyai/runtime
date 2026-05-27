-- SPDX-License-Identifier: MPL-2.0

-- Mid-tier worker in a process chain. Inherits the ambient overlay from its
-- parent's context and spawns a grandchild (overlay_worker) with NO explicit
-- network option. The grandchild must inherit the same overlay transitively.
-- Grandchild's http.get result bubbles back to the test via our return value.

local time = require("time")

local function main()
	local events_ch = process.events()

	local _, err = process.spawn_monitored(
		"app.test.network:overlay_worker",
		"app:processes"
	)
	if err ~= nil then
		return { ok = false, err = "spawn grandchild: " .. tostring(err) }
	end

	local timeout = time.after("5s")
	local sel = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}
	if sel.channel == timeout then
		return { ok = false, err = "timeout waiting for grandchild" }
	end

	local event = sel.value
	if event.kind ~= process.event.EXIT then
		return { ok = false, err = "unexpected event: " .. tostring(event.kind) }
	end
	if event.result.error ~= nil then
		return { ok = false, err = "grandchild crashed: " .. tostring(event.result.error) }
	end
	return event.result.value
end

return { main = main }
