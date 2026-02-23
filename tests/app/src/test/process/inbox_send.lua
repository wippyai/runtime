-- SPDX-License-Identifier: MPL-2.0

-- Test: Send message to process via custom topic
local assert = require("assert2")
local time = require("time")

local function main()
-- Test send and listen exist
	assert.not_nil(process.send, "process.send exists")
	assert.not_nil(process.listen, "process.listen exists")

	-- Monitor worker to wait for completion
	local events_ch = process.events()

	-- Spawn worker that listens on a custom topic
	local child_pid, err = process.spawn_monitored("app.test.process:listen_worker", "app:processes")
	assert.is_nil(err, "spawn listen_worker no error")
	assert.not_nil(child_pid, "got child pid")

	-- Send message immediately - no sleep, message must be queued
	local sent, send_err = process.send(child_pid, "messages", "hello from parent")
	assert.is_nil(send_err, "send no error")
	assert.ok(sent, "send succeeded")

	-- Wait for worker to exit (with timeout)
	local timeout = time.after("2s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for worker to exit"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, child_pid, "event from child")

	return true
end

return { main = main }
