-- Test: Verify process that calls error() dies properly
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()
	assert.not_nil(events_ch, "got events channel")

	-- Spawn a worker that immediately errors
	local worker_pid, err = process.spawn_monitored("app.test.process:error_exit_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Wait for EXIT event
	local timeout = time.after("3s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for EXIT event"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, worker_pid, "event from worker")

	-- Verify worker is dead
	local ok, send_err = process.send(worker_pid, "test", "hello")
	if ok then
		return false, "worker should be dead but send succeeded"
	end

	if not send_err or not string.find(send_err, "not found") then
		return false, "expected 'not found' error, got: " .. tostring(send_err)
	end

	return true
end

return { main = main }
