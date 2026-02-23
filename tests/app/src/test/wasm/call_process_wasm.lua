-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local time = require("time")

local function main()
	local events = process.events()
	assert.not_nil(events, "events channel should be available")

	local child_pid, spawn_err = process.spawn_monitored(
		"app.test.wasm:compute_component_process",
		"app:processes",
		6,
		7
	)
	assert.is_nil(spawn_err, "spawn_monitored should not error")
	assert.not_nil(child_pid, "spawn_monitored should return child pid")

	local timeout = time.after("3s")
	local selected = channel.select({
		events:case_receive(),
		timeout:case_receive(),
	})

	if selected.channel ~= events then
		return false, "timeout waiting for process EXIT event"
	end

	local evt = selected.value
	assert.eq(evt.kind, process.event.EXIT, "expected EXIT event")
	assert.eq(evt.from, child_pid, "expected EXIT from spawned pid")
	assert.not_nil(evt.result, "EXIT event should include result")
	assert.is_nil(evt.result.error, "process should finish without runtime error")
	assert.eq(evt.result.value, 42, "process result should match compute output")

	return true
end

return { main = main }
