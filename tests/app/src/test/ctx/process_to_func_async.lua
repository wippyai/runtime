-- SPDX-License-Identifier: MPL-2.0

-- Test: funcs.async from inside a process returns a real future payload.
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	local child_pid, err = process.spawn_monitored("app.test.ctx:process_calls_func_async_worker", "app:processes")
	assert.is_nil(err, "spawn no error")
	assert.not_nil(child_pid, "spawn returns pid")

	local timeout = time.after("3s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for async worker"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.is_nil(event.error, "worker exited without error")
	assert.eq(event.result.value, true, "worker returned true")

	return true
end

return { main = main }
