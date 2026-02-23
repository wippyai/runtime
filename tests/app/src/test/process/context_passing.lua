-- SPDX-License-Identifier: MPL-2.0

-- Test: Context passing through process.with_context spawn
local assert = require("assert2")
local time = require("time")

local function main()
-- Test context passing through process.with_context():spawn_monitored()
-- The worker will read context values and error if they don't match expected
	local events_ch = process.events()

	local spawner = process.with_context({
		request_id = "req-123",
		user_id = 42,
		is_admin = true
	})
	assert.not_nil(spawner, "with_context returns spawner")

	-- Spawn worker that validates context values
	local child_pid, err = spawner:spawn_monitored("app.test.process:context_validator_worker", "app:processes")
	assert.is_nil(err, "spawn with context no error")
	assert.not_nil(child_pid, "spawn with context returns pid")

	-- Wait for worker to exit
	local timeout = time.after("2s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for worker exit"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, child_pid, "event from child")
	assert.is_nil(event.error, "worker exited without error (context was correct)")

	-- Test chaining with_context calls
	local spawner2 = process.with_context({ key1 = "value1" })
	:with_context({ key2 = "value2" })

	local child2_pid, err2 = spawner2:spawn_monitored("app.test.process:context_kv_validator_worker", "app:processes")
	assert.is_nil(err2, "chained spawn no error")
	assert.not_nil(child2_pid, "chained spawn returns pid")

	timeout = time.after("2s")
	result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for second worker exit"
	end

	event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event for chained")
	assert.is_nil(event.error, "chained worker exited without error (context was correct)")

	return true
end

return { main = main }
