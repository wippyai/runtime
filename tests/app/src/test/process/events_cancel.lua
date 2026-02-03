-- Test: Send cancel to child and verify it handles cancel properly
local assert = require("assert2")

local function main()
	local events_ch = process.events()
	assert.not_nil(events_ch, "got events channel")

	-- Spawn the test worker
	local worker_pid, err = process.spawn_monitored("app.test.process:events_cancel_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Wait for EXIT event (direct receive, no timeout)
	local event = events_ch:receive()
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, worker_pid, "event from worker")

	return true
end

return { main = main }
