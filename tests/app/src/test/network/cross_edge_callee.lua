-- SPDX-License-Identifier: MPL-2.0

-- Middle link in the func -> process -> func chain. Runs under an ambient
-- overlay set by the outer funcs.with_options({network=...}). Spawns a
-- process with NO options; the spawned worker in turn makes a nested
-- funcs.call that ultimately issues the http.get. The overlay must survive
-- both boundary crossings (funcs->process and process->funcs).

local time = require("time")

local function main()
	local events_ch = process.events()

	local _, err = process.spawn_monitored(
		"app.test.network:cross_edge_worker",
		"app:processes"
	)
	if err ~= nil then
		return { ok = false, err = "spawn cross_edge_worker: " .. tostring(err) }
	end

	local timeout = time.after("10s")
	local sel = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}
	if sel.channel == timeout then
		return { ok = false, err = "timeout waiting for cross_edge_worker" }
	end

	local event = sel.value
	if event.kind ~= process.event.EXIT then
		return { ok = false, err = "unexpected event: " .. tostring(event.kind) }
	end
	if event.result.error ~= nil then
		return { ok = false, err = "worker crashed: " .. tostring(event.result.error) }
	end
	return event.result.value
end

return { main = main }
