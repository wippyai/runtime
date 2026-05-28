-- SPDX-License-Identifier: MPL-2.0

-- Test: Verify CANCEL event is received by child process via events()
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn worker that waits for CANCEL and verifies it
	local worker_pid, err = process.spawn_monitored("app.test.process:cancel_verify_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Give worker time to call process.events()
	time.sleep("10ms")

	-- Cancel the worker
	local _, cancel_err = process.cancel(worker_pid)
	assert.is_nil(cancel_err, "cancel no error")

	-- Wait for EXIT event
	local timeout = time.after("5s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for worker EXIT"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, worker_pid, "EXIT from worker")

	return true
end

return { main = main }
