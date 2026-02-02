-- Test: Names are automatically released when process exits
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()
	local test_name = "auto_release_" .. tostring(os.time())

	-- Spawn worker that registers itself then exits
	local worker_pid, err = process.spawn_monitored(
		"app.test.process:registry_exit_worker",
		"app:processes"
	)
	assert.is_nil(err, "spawn worker no error")

	-- Send name to register
	process.send(worker_pid, "register_and_exit", { name = test_name })

	-- Wait for worker to exit
	local timeout = time.after("2s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for worker EXIT"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, worker_pid, "event from worker")

	-- Small delay to ensure cleanup has propagated
	time.sleep("50ms")

	-- Now lookup should fail - name should be released
	local pid, lookup_err = process.registry.lookup(test_name)
	assert.is_nil(pid, "lookup returns nil after process exit")
	assert.not_nil(lookup_err, "lookup has error after process exit")

	-- Send to the name should also fail
	local ok
	ok, err = process.send(test_name, "test", "data")
	assert.is_nil(ok, "send to released name fails")
	assert.not_nil(err, "send error for released name")

	return true
end

return { main = main }
