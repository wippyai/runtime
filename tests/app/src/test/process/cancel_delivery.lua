-- SPDX-License-Identifier: MPL-2.0

-- Test: Verify CANCEL event is properly delivered
-- Note: This test spawns a worker that will receive CANCEL and verifies via EXIT event
local assert = require("assert2")
local time = require("time")

local function main()
-- Spawn a long-running worker that waits for cancel
	local worker_pid, err = process.spawn_monitored("app.test.process:long_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	local events_ch = process.events()

	-- Give worker time to start
	time.sleep("5ms")

	-- Cancel the worker with 100ms timeout
	local ok, cancel_err = process.cancel(worker_pid)
	assert.is_nil(cancel_err, "cancel no error")

	-- Wait for EXIT event from the cancelled worker
	local event = events_ch:receive()
	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT event, got: " .. tostring(event.kind)
	end

	if event.from ~= worker_pid then
		return false, "expected event from worker " .. worker_pid .. ", got: " .. tostring(event.from)
	end

	-- Verify worker is dead by trying to send to it
	ok, err = process.send(worker_pid, "test", "hello")
	if ok then
		return false, "worker should be dead but send succeeded"
	end

	if not err or not string.find(err, "not found") then
		return false, "expected 'not found' error, got: " .. tostring(err)
	end

	return true
end

return { main = main }
