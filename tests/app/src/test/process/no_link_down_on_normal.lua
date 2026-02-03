-- Test: LINK_DOWN is NOT sent when linked process exits normally
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()
	assert.not_nil(events_ch, "got events channel")

	local worker_pid, err = process.spawn_monitored("app.test.process:verify_no_link_down_on_normal_worker", "app:processes")
	assert.is_nil(err, "spawn worker no error")
	assert.not_nil(worker_pid, "got worker pid")

	-- Wait for EXIT event
	local timeout = time.after("3s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for EXIT event"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, worker_pid, "event from worker")

	return true
end

return { main = main }
