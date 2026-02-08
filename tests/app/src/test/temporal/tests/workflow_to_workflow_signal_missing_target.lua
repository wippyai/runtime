local assert = require("assert2")
local errors = require("errors")
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

	-- Temporal PID shape is node|uniqid; use impossible uniqid to force NotFound.
	local missing_target = "app.test.temporal:test_client@app.test.temporal:test_worker|__missing_workflow_target__"

	local sender_pid, sender_err = process.spawn_monitored(
		"app.test.temporal.workflows:workflow_signal_peer_workflow",
		"app.test.temporal:test_worker",
		{
			target_pid = missing_target,
			topic = "peer_ping",
			payload = { source = "missing_target_probe" },
		}
	)
	assert.is_nil(sender_err, "spawn sender workflow no error")
	assert.is_string(sender_pid, "got sender pid")

	local sender_exit, exit_err = wait_for_exit(events_ch, sender_pid, "5s")
	assert.is_nil(exit_err, exit_err)
	if sender_exit == nil then
		error("missing sender exit event")
	end

	assert.is_table(sender_exit.result.value, "sender result value table")
	assert.eq(sender_exit.result.value.ok, false, "send to missing target should fail")
	assert.eq(sender_exit.result.value.error_kind, errors.NOT_FOUND, "missing target maps to NOT_FOUND")
	assert.eq(sender_exit.result.value.error_retryable, false, "missing target error is not retryable")
	assert.contains(tostring(sender_exit.result.value.error), "workflow", "missing target error contains workflow context")

	return true
end

return { main = main }
