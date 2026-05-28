-- SPDX-License-Identifier: MPL-2.0

-- Test: Unmonitor stops receiving EXIT events
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn monitored worker
	local worker_pid, err = process.spawn_monitored("app.test.process:long_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Give worker time to start
	time.sleep("5ms")

	-- Unmonitor it
	local _, unmon_err = process.unmonitor(worker_pid)
	if unmon_err then
		return false, "unmonitor failed: " .. tostring(unmon_err)
	end

	-- Cancel the worker
	_, err = process.cancel(worker_pid)
	assert.is_nil(err, "cancel no error")

	-- Should NOT receive EXIT because we unmonitored
	local timeout = time.after("200ms")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == events_ch then
		return false, "should not receive EXIT after unmonitor, got: " .. tostring(result.value.kind)
	end

	-- Timeout is expected - worker exited but we didn't get notified
	return true
end

return { main = main }
