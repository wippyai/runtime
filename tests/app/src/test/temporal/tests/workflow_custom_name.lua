-- SPDX-License-Identifier: MPL-2.0

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

	local bare_pid, bare_err = process.spawn_monitored(
		"ExternalNamedWorkflow",
		"app.test.temporal:test_worker",
		{ name = "Bare" }
	)
	assert.is_nil(bare_err, "spawn by bare custom name should not error")
	assert.is_string(bare_pid, "got pid for bare-name spawn")

	local bare_event, bare_wait = wait_for_exit(events_ch, bare_pid, "5s")
	assert.is_nil(bare_wait, bare_wait)
	assert.is_nil(bare_event.result.error, "bare-name workflow exited without error")
	assert.is_table(bare_event.result.value, "bare-name result table")
	assert.eq(bare_event.result.value.workflow, "ExternalNamedWorkflow", "bare name dispatched to the registered named workflow")
	assert.contains(bare_event.result.value.message, "Bare", "bare-name message contains input")

	local opt_pid, opt_err = process.with_options({
		["workflow.name"] = "ExternalNamedWorkflow",
	}):spawn_monitored(
		"app.test.temporal.workflows:hello_workflow",
		"app.test.temporal:test_worker",
		{ name = "ViaOption" }
	)
	assert.is_nil(opt_err, "spawn with workflow.name option should not error")
	assert.is_string(opt_pid, "got pid for option-override spawn")

	local opt_event, opt_wait = wait_for_exit(events_ch, opt_pid, "5s")
	assert.is_nil(opt_wait, opt_wait)
	assert.is_nil(opt_event.result.error, "option-override workflow exited without error")
	assert.is_table(opt_event.result.value, "option-override result table")
	assert.eq(opt_event.result.value.workflow, "ExternalNamedWorkflow", "workflow.name option overrode the source id")
	assert.contains(opt_event.result.value.message, "ViaOption", "option-override message contains input")

	return true
end

return { main = main }
