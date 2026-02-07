local assert = require("assert2")
local time = require("time")

local function wait_for_exit(events_ch, pid, timeout)
	local deadline = time.after(timeout or "5s")
	while true do
		local result = channel.select {
			events_ch:case_receive(),
			deadline:case_receive(),
		}
		if result.channel == deadline then
			return nil, "timeout waiting for exit"
		end
		local event = result.value
		if event.from == pid and event.kind == process.event.EXIT then
			return event, nil
		end
	end
end

local function main()
	local events_ch = process.events()
	assert.not_nil(events_ch, "got events channel")

	local pid, err = process.spawn_monitored(
		"app.test.temporal.workflows:workflow_link_unsupported_probe",
		"app.test.temporal:test_worker"
	)
	assert.is_nil(err, "spawn probe workflow no error")
	assert.is_string(pid, "got workflow pid")

	local event, wait_err = wait_for_exit(events_ch, pid, "5s")
	assert.is_nil(wait_err, wait_err)
	if event == nil then
		error("missing exit event")
	end

	assert.is_table(event.result.value, "probe result is table")
	assert.eq(event.result.value.pid_ok, true, "process.pid works in workflow")
	assert.eq(event.result.value.link_ok, nil, "process.link does not return success in workflow")
	assert.contains(
		tostring(event.result.value.link_error),
		"not supported in workflow context",
		"link should be rejected with workflow-context error"
	)

	return true
end

return { main = main }

