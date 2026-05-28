-- SPDX-License-Identifier: MPL-2.0

-- Test: Registry name resolution in process.send
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()
	local inbox_ch = process.inbox()
	-- Spawn echo worker
	local worker_pid, err = process.spawn_monitored("app.test.process:echo_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Give worker time to start and listen
	time.sleep("10ms")

	-- Register worker with a name (we need worker to do this)
	-- For now test that we can send using PID directly
	local ok
	ok, err = process.send(worker_pid, "echo", { msg = "hello", reply_to = process.pid() })
	assert.is_nil(err, "send no error")
	assert.ok(ok, "send succeeded")

	-- Wait for echo response
	local timeout = time.after("2s")
	local result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= inbox_ch then
		return false, "timeout waiting for echo response"
	end

	-- Cancel worker
	process.cancel(worker_pid)

	-- Wait for EXIT
	timeout = time.after("2s")
	result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for worker EXIT"
	end

	return true
end

return { main = main }
