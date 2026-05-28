-- SPDX-License-Identifier: MPL-2.0

-- Test: Explicit monitor/unmonitor functions
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn a short worker WITHOUT monitoring initially
	local worker_pid, err = process.spawn("app.test.process:long_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Explicitly monitor it
	local _, monitor_err = process.monitor(worker_pid)
	if monitor_err then
		return false, "monitor failed: " .. tostring(monitor_err)
	end

	-- Give worker time to start
	time.sleep("5ms")

	-- Cancel the worker
	_, err = process.cancel(worker_pid)
	assert.is_nil(err, "cancel no error")

	-- Should receive EXIT because we monitored
	local timeout = time.after("2s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout - did not receive EXIT for monitored process"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, worker_pid, "EXIT from worker")

	return true
end

return { main = main }
