-- Test: process.unlisten stops receiving messages on topic
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn worker that unlistens and verifies no more messages
	local worker_pid, err = process.spawn_monitored("app.test.process:unlisten_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Give worker time to subscribe
	time.sleep("10ms")

	-- Send first message - should be received
	local _, send_err = process.send(worker_pid, "test_topic", "msg1")
	assert.is_nil(send_err, "send msg1 no error")

	-- Wait for worker to process and unlisten
	time.sleep("50ms")

	-- Send second message - should NOT be received after unlisten
	_, send_err = process.send(worker_pid, "test_topic", "msg2")
	assert.is_nil(send_err, "send msg2 no error")

	-- Signal worker to finish via inbox
	_, send_err = process.send(worker_pid, "inbox", "done")
	assert.is_nil(send_err, "send done no error")

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
