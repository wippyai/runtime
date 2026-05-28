-- SPDX-License-Identifier: MPL-2.0

-- Test: Combine inbox, events, and listen in single select
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn worker that uses all three channel types
	local worker_pid, err = process.spawn_monitored("app.test.process:combined_channels_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Give worker time to subscribe
	time.sleep("10ms")

	-- Send to custom topic first
	local _, send_err = process.send(worker_pid, "custom", "custom_msg")
	assert.is_nil(send_err, "send custom no error")

	-- Send to inbox
	_, send_err = process.send(worker_pid, "inbox", "inbox_msg")
	assert.is_nil(send_err, "send inbox no error")

	-- Wait a bit then cancel (will trigger events channel)
	time.sleep("50ms")
	_, err = process.cancel(worker_pid)
	assert.is_nil(err, "cancel no error")

	-- Wait for EXIT
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

	-- Check worker didn't fail
	if event.error then
		return false, "worker failed: " .. tostring(event.error)
	end

	return true
end

return { main = main }
