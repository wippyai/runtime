-- Test: process.unlink explicit function
local assert = require("assert2")
local time = require("time")

local function main()
-- Test unlink exists
	assert.not_nil(process.unlink, "process.unlink exists")
	assert.is_function(process.unlink, "process.unlink is function")

	local events_ch = process.events()
	local inbox_ch = process.inbox()
	local my_pid = process.pid()

	-- Spawn worker that will link then unlink
	local worker_pid, err = process.spawn_monitored(
		"app.test.process:unlink_test_worker",
		"app:processes"
	)
	assert.is_nil(err, "spawn worker no error")

	-- Send our PID to worker
	process.send(worker_pid, "inbox", my_pid)

	-- Wait for worker to confirm link
	local timeout = time.after("2s")
	local result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}
	assert.neq(result.channel, timeout, "received link confirmation")
	assert.eq(result.value:topic(), "linked", "got linked message")

	-- Tell worker to unlink
	process.send(worker_pid, "unlink", true)

	-- Wait for unlink confirmation
	timeout = time.after("2s")
	result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}
	assert.neq(result.channel, timeout, "received unlink confirmation")
	assert.eq(result.value:topic(), "unlinked", "got unlinked message")

	-- Wait for worker EXIT
	timeout = time.after("2s")
	result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}
	assert.neq(result.channel, timeout, "received worker exit")
	assert.eq(result.value.kind, process.event.EXIT, "got EXIT event")

	-- Verify worker returned NO_LINK_DOWN (didn't receive LINK_DOWN after unlink)
	-- Result is wrapped in {value=...} table
	local result_value = result.value.result
	if type(result_value) == "table" then
		result_value = result_value.value
	end
	assert.eq(result_value, "NO_LINK_DOWN", "worker did not receive LINK_DOWN after unlink")

	return true
end

return { main = main }
