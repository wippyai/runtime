-- SPDX-License-Identifier: MPL-2.0

-- Test: Send to a registered name without resolving PID first
local assert = require("assert2")
local time = require("time")

local function main()
	local inbox_ch = process.inbox()
	local events_ch = process.events()
	local test_name = "echo_service_" .. tostring(os.time())

	-- Spawn worker that will register itself with a name
	local worker_pid, err = process.spawn_monitored(
		"app.test.process:registry_echo_worker",
		"app:processes"
	)
	assert.is_nil(err, "spawn worker no error")

	-- Send the name to register to the worker
	process.send(worker_pid, "register", { name = test_name, reply_to = process.pid() })

	-- Wait for worker to confirm registration
	local timeout = time.after("2s")
	local result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for registration confirmation"
	end

	local msg = result.value
	if msg:topic() ~= "registered" then
		return false, "expected 'registered' topic, got: " .. tostring(msg:topic())
	end

	-- Now send directly to the NAME (not PID)
	local ok
	ok, err = process.send(test_name, "echo", { value = "hello_via_name", reply_to = process.pid() })
	assert.is_nil(err, "send to name no error")
	assert.ok(ok, "send to name succeeded")

	-- Wait for echo response
	timeout = time.after("2s")
	result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for echo response"
	end

	msg = result.value
	assert.eq(msg:topic(), "echo_reply", "got echo_reply topic")
	local payload = msg:payload():data()
	assert.eq(payload.echoed, "hello_via_name", "got correct echoed value")

	-- Cleanup: cancel worker
	process.cancel(worker_pid)

	-- Wait for EXIT
	timeout = time.after("2s")
	result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for worker EXIT"
	end

	return true
end

return { main = main }
