-- SPDX-License-Identifier: MPL-2.0

-- Test: Process listens on multiple topics simultaneously
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn worker that listens on multiple topics
	local worker_pid, err = process.spawn_monitored("app.test.process:multi_topic_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Give worker time to subscribe to both topics
	time.sleep("10ms")

	-- Send to topic A
	local _, send_err = process.send(worker_pid, "topic_a", "msg_a")
	assert.is_nil(send_err, "send topic_a no error")

	-- Send to topic B
	_, send_err = process.send(worker_pid, "topic_b", "msg_b")
	assert.is_nil(send_err, "send topic_b no error")

	-- Wait for worker to exit
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
